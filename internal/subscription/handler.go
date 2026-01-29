package subscription

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

type PlanInfo struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Price                  float64 `json:"price"`
	MaxUsers               int     `json:"max_users"`
	MaxProducts            int     `json:"max_products"`
	MaxTransactionsDaily   int     `json:"max_transactions_daily"`
	MaxTransactionsMonthly int     `json:"max_transactions_monthly"`
	MaxOutlets             int     `json:"max_outlets"`
	DataRetentionDays      int     `json:"data_retention_days"`
	Features               []string `json:"features"`
}

var Plans = map[string]PlanInfo{
	"gratis": {
		ID:                     "gratis",
		Name:                   "Gratis",
		Price:                  0,
		MaxUsers:               1,
		MaxProducts:            50,
		MaxTransactionsDaily:   30,
		MaxTransactionsMonthly: 0,
		MaxOutlets:             1,
		DataRetentionDays:      30,
		Features:               []string{"POS dasar", "1 pengguna", "50 produk", "30 transaksi/hari", "QRIS", "Diskon & promo"},
	},
	"pemula": {
		ID:                     "pemula",
		Name:                   "Pemula",
		Price:                  49000,
		MaxUsers:               3,
		MaxProducts:            200,
		MaxTransactionsDaily:   0, // unlimited
		MaxTransactionsMonthly: 0, // unlimited
		MaxOutlets:             1,
		DataRetentionDays:      365,
		Features:               []string{"Semua fitur Gratis", "3 pengguna", "200 produk", "Unlimited transaksi", "Kelola bahan baku", "Auto-deduct ingredien", "Custom logo struk", "Laporan laba/rugi", "Export CSV/Excel"},
	},
	"bisnis": {
		ID:                     "bisnis",
		Name:                   "Bisnis",
		Price:                  149000,
		MaxUsers:               10,
		MaxProducts:            0, // unlimited
		MaxTransactionsDaily:   0,
		MaxTransactionsMonthly: 0,
		MaxOutlets:             3,
		DataRetentionDays:      365 * 3,
		Features:               []string{"Semua fitur Pemula", "10 pengguna", "Unlimited produk", "3 outlet", "Role Manager", "Laporan per-outlet", "Log aktivitas staff", "WhatsApp support"},
	},
	"enterprise": {
		ID:                     "enterprise",
		Name:                   "Enterprise",
		Price:                  0, // Custom
		MaxUsers:               0, // unlimited
		MaxProducts:            0,
		MaxTransactionsDaily:   0,
		MaxTransactionsMonthly: 0,
		MaxOutlets:             0, // unlimited
		DataRetentionDays:      0, // forever
		Features:               []string{"Semua fitur Bisnis", "Unlimited semua", "Inventori terpusat", "Account manager", "SLA support", "Custom integrasi"},
	},
}

// GetPlans returns all available plans
func (h *Handler) GetPlans(c *gin.Context) {
	plans := []PlanInfo{}
	for _, plan := range []string{"gratis", "pemula", "bisnis", "enterprise"} {
		plans = append(plans, Plans[plan])
	}
	c.JSON(http.StatusOK, gin.H{"data": plans})
}

// GetCurrent returns current subscription
func (h *Handler) GetCurrent(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var subscription database.Subscription
	if err := h.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
		// Create default subscription if not exists
		tenantUUID, _ := uuid.Parse(tenantID)
		subscription = database.Subscription{
			TenantID:           tenantUUID,
			Plan:               "gratis",
			Status:             "active",
			MaxUsers:           1,
			MaxProducts:        20,
			MaxTransactionsDaily: 20,
			MaxOutlets:         1,
			DataRetentionDays:  30,
			CurrentPeriodStart: time.Now(),
			CurrentPeriodEnd:   time.Now().AddDate(0, 1, 0),
		}
		h.db.Create(&subscription)
	}

	// Get plan details
	plan := Plans[subscription.Plan]

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"subscription":       subscription,
			"plan":               plan,
			"current_period_end": subscription.CurrentPeriodEnd,
		},
	})
}

// GetUsage returns current usage stats
func (h *Handler) GetUsage(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var subscription database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&subscription)

	// Count current usage
	var userCount int64
	h.db.Model(&database.User{}).Where("tenant_id = ?", tenantID).Count(&userCount)

	var productCount int64
	h.db.Model(&database.Product{}).Where("tenant_id = ? AND is_active = ?", tenantID, true).Count(&productCount)

	// Today's transactions
	today := time.Now().Truncate(24 * time.Hour)
	var todayTxCount int64
	h.db.Model(&database.Transaction{}).
		Where("tenant_id = ? AND created_at >= ?", tenantID, today).
		Count(&todayTxCount)

	// This month's transactions
	startOfMonth := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Now().Location())
	var monthTxCount int64
	h.db.Model(&database.Transaction{}).
		Where("tenant_id = ? AND created_at >= ?", tenantID, startOfMonth).
		Count(&monthTxCount)

	var outletCount int64
	h.db.Model(&database.Outlet{}).Where("tenant_id = ?", tenantID).Count(&outletCount)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"users":                   userCount,
			"max_users":               subscription.MaxUsers,
			"products":                productCount,
			"max_products":            subscription.MaxProducts,
			"transactions_today":      todayTxCount,
			"max_transactions_daily":  subscription.MaxTransactionsDaily,
			"transactions_month":      monthTxCount,
			"max_transactions_monthly": subscription.MaxTransactionsMonthly,
			"outlets":                 outletCount,
			"max_outlets":             subscription.MaxOutlets,
		},
	})
}

type UpgradeRequest struct {
	Plan string `json:"plan" binding:"required"`
}

// Upgrade request to change plan (simplified - real implementation needs payment)
func (h *Handler) Upgrade(c *gin.Context) {
	var req UpgradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	plan, ok := Plans[req.Plan]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid plan"})
		return
	}

	tenantID := c.GetString("tenant_id")

	var subscription database.Subscription
	if err := h.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscription not found"})
		return
	}

	// Migrate products/materials/users with NULL outlet_id to first outlet
	var firstOutlet database.Outlet
	if err := h.db.Where("tenant_id = ?", tenantID).Order("created_at ASC").First(&firstOutlet).Error; err == nil {
		h.db.Model(&database.Product{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		h.db.Model(&database.RawMaterial{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		h.db.Model(&database.User{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)
	}

	// Update subscription
	subscription.Plan = req.Plan
	subscription.MaxUsers = plan.MaxUsers
	subscription.MaxProducts = plan.MaxProducts
	subscription.MaxTransactionsDaily = plan.MaxTransactionsDaily
	subscription.MaxTransactionsMonthly = plan.MaxTransactionsMonthly
	subscription.MaxOutlets = plan.MaxOutlets
	subscription.DataRetentionDays = plan.DataRetentionDays
	subscription.CurrentPeriodStart = time.Now()
	subscription.CurrentPeriodEnd = time.Now().AddDate(0, 1, 0)

	h.db.Save(&subscription)

	c.JSON(http.StatusOK, gin.H{
		"data": subscription,
		"message": "Subscription upgraded successfully",
	})
}
