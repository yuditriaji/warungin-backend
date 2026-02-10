package subscription

import (
	"fmt"
	"time"

	"github.com/yuditriaji/warungin-backend/pkg/database"
	"github.com/yuditriaji/warungin-backend/pkg/email"
	"gorm.io/gorm"
)

// Scheduler runs background jobs for subscription lifecycle management
type Scheduler struct {
	db *gorm.DB
}

// NewScheduler creates a new subscription scheduler
func NewScheduler(db *gorm.DB) *Scheduler {
	return &Scheduler{db: db}
}

// Start begins the scheduler loop (runs every hour)
func (s *Scheduler) Start() {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		// Run immediately on startup
		s.Run()

		for range ticker.C {
			s.Run()
		}
	}()
	fmt.Println("Subscription scheduler started (runs every 1 hour)")
}

// Run executes all scheduled jobs
func (s *Scheduler) Run() {
	fmt.Println("Running subscription scheduler...")
	s.SendExpiryReminders()
	s.DowngradeExpiredSubscriptions()
	fmt.Println("Subscription scheduler completed")
}

// SendExpiryReminders sends email reminders for subscriptions nearing expiry
func (s *Scheduler) SendExpiryReminders() {
	emailService := email.NewEmailService()
	if !emailService.IsConfigured() {
		fmt.Println("Scheduler: Email service not configured, skipping reminders")
		return
	}

	now := time.Now()
	reminderDays := []int{7, 3, 1}

	for _, days := range reminderDays {
		targetDate := now.AddDate(0, 0, days)
		dayStart := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, targetDate.Location())
		dayEnd := dayStart.AddDate(0, 0, 1)

		var subscriptions []database.Subscription
		s.db.Where(
			"plan != ? AND status = ? AND current_period_end >= ? AND current_period_end < ?",
			"gratis", "active", dayStart, dayEnd,
		).Find(&subscriptions)

		for _, sub := range subscriptions {
			// Get tenant owner email
			var user database.User
			if err := s.db.Where("tenant_id = ? AND role = ?", sub.TenantID, "owner").First(&user).Error; err != nil {
				continue
			}

			if user.Email == "" {
				continue
			}

			// Get tenant name
			var tenant database.Tenant
			s.db.Where("id = ?", sub.TenantID).First(&tenant)

			planName := getPlanName(sub.Plan)
			expiryDate := sub.CurrentPeriodEnd.Format("2 January 2006")

			if sub.CancelledAt != nil {
				// Subscription was cancelled — ending notice
				if err := emailService.SendSubscriptionEndingEmail(
					user.Email, user.Name, tenant.Name,
					planName, expiryDate, days,
				); err != nil {
					fmt.Printf("Scheduler: Failed to send ending email to %s: %v\n", user.Email, err)
				} else {
					fmt.Printf("Scheduler: Sent subscription ending email (%dd) to %s\n", days, user.Email)
				}
			} else {
				// Active subscription — renewal reminder
				if err := emailService.SendExpiryReminderEmail(
					user.Email, user.Name, tenant.Name,
					planName, expiryDate, days,
				); err != nil {
					fmt.Printf("Scheduler: Failed to send reminder email to %s: %v\n", user.Email, err)
				} else {
					fmt.Printf("Scheduler: Sent expiry reminder (%dd) to %s\n", days, user.Email)
				}
			}
		}
	}
}

// DowngradeExpiredSubscriptions downgrades expired paid subscriptions to Gratis
func (s *Scheduler) DowngradeExpiredSubscriptions() {
	emailService := email.NewEmailService()
	now := time.Now()

	var subscriptions []database.Subscription
	s.db.Where(
		"plan != ? AND status = ? AND current_period_end < ?",
		"gratis", "active", now,
	).Find(&subscriptions)

	for _, sub := range subscriptions {
		previousPlan := getPlanName(sub.Plan)

		// Downgrade to gratis
		sub.Plan = "gratis"
		sub.Status = "active"
		sub.MaxUsers = 1
		sub.MaxProducts = 50
		sub.MaxTransactionsDaily = 30
		sub.MaxTransactionsMonthly = 0
		sub.MaxOutlets = 1
		sub.DataRetentionDays = 30
		sub.CancelledAt = nil
		sub.AutoRenew = true
		sub.BillingPeriod = "monthly"
		s.db.Save(&sub)

		fmt.Printf("Scheduler: Auto-downgraded tenant %s from %s to Gratis\n", sub.TenantID, previousPlan)

		// Send downgrade notification email
		if emailService.IsConfigured() {
			var user database.User
			if err := s.db.Where("tenant_id = ? AND role = ?", sub.TenantID, "owner").First(&user).Error; err == nil && user.Email != "" {
				var tenant database.Tenant
				s.db.Where("id = ?", sub.TenantID).First(&tenant)

				if err := emailService.SendDowngradeNotificationEmail(
					user.Email, user.Name, tenant.Name, previousPlan,
				); err != nil {
					fmt.Printf("Scheduler: Failed to send downgrade email to %s: %v\n", user.Email, err)
				}
			}
		}
	}

	if len(subscriptions) > 0 {
		fmt.Printf("Scheduler: Downgraded %d expired subscription(s)\n", len(subscriptions))
	}
}

// getPlanName returns the display name for a plan
func getPlanName(plan string) string {
	names := map[string]string{
		"gratis":     "Gratis",
		"pemula":     "Pemula",
		"bisnis":     "Bisnis",
		"enterprise": "Enterprise",
	}
	if name, ok := names[plan]; ok {
		return name
	}
	return plan
}
