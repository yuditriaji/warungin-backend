package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

// LimitChecker provides methods to check subscription limits
type LimitChecker struct {
	db *gorm.DB
}

func NewLimitChecker(db *gorm.DB) *LimitChecker {
	return &LimitChecker{db: db}
}

// CheckProductLimit middleware checks if tenant can create more products
func (l *LimitChecker) CheckProductLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only check on POST (create)
		if c.Request.Method != "POST" {
			c.Next()
			return
		}

		tenantID := c.GetString("tenant_id")

		// Get subscription
		var subscription database.Subscription
		if err := l.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
			c.Next()
			return
		}

		// Check if unlimited (0 means unlimited)
		if subscription.MaxProducts == 0 {
			c.Next()
			return
		}

		// Count current products
		var productCount int64
		l.db.Model(&database.Product{}).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			Count(&productCount)

		if int(productCount) >= subscription.MaxProducts {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "Product limit reached",
				"message": "Batas produk tercapai. Upgrade paket untuk menambah lebih banyak produk.",
				"code":    "LIMIT_PRODUCTS",
				"current": productCount,
				"limit":   subscription.MaxProducts,
			})
			return
		}

		c.Next()
	}
}

// CheckTransactionLimit middleware checks daily/monthly transaction limits
func (l *LimitChecker) CheckTransactionLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only check on POST (create)
		if c.Request.Method != "POST" {
			c.Next()
			return
		}

		tenantID := c.GetString("tenant_id")

		// Get subscription
		var subscription database.Subscription
		if err := l.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
			c.Next()
			return
		}

		// Check daily limit (for gratis tier)
		if subscription.MaxTransactionsDaily > 0 {
			today := time.Now().Truncate(24 * time.Hour)
			var todayCount int64
			l.db.Model(&database.Transaction{}).
				Where("tenant_id = ? AND created_at >= ?", tenantID, today).
				Count(&todayCount)

			if int(todayCount) >= subscription.MaxTransactionsDaily {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":   "Daily transaction limit reached",
					"message": "Batas transaksi harian tercapai. Upgrade paket untuk transaksi unlimited.",
					"code":    "LIMIT_DAILY_TX",
					"current": todayCount,
					"limit":   subscription.MaxTransactionsDaily,
				})
				return
			}
		}

		// Check monthly limit
		if subscription.MaxTransactionsMonthly > 0 {
			startOfMonth := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Now().Location())
			var monthCount int64
			l.db.Model(&database.Transaction{}).
				Where("tenant_id = ? AND created_at >= ?", tenantID, startOfMonth).
				Count(&monthCount)

			if int(monthCount) >= subscription.MaxTransactionsMonthly {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":   "Monthly transaction limit reached",
					"message": "Batas transaksi bulanan tercapai. Upgrade paket untuk lebih banyak transaksi.",
					"code":    "LIMIT_MONTHLY_TX",
					"current": monthCount,
					"limit":   subscription.MaxTransactionsMonthly,
				})
				return
			}
		}

		c.Next()
	}
}

// CheckUserLimit middleware checks if tenant can create more users
func (l *LimitChecker) CheckUserLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != "POST" {
			c.Next()
			return
		}

		tenantID := c.GetString("tenant_id")

		var subscription database.Subscription
		if err := l.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
			c.Next()
			return
		}

		if subscription.MaxUsers == 0 {
			c.Next()
			return
		}

		var userCount int64
		l.db.Model(&database.User{}).Where("tenant_id = ?", tenantID).Count(&userCount)

		if int(userCount) >= subscription.MaxUsers {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "User limit reached",
				"message": "Batas pengguna tercapai. Upgrade paket untuk menambah pengguna.",
				"code":    "LIMIT_USERS",
				"current": userCount,
				"limit":   subscription.MaxUsers,
			})
			return
		}

		c.Next()
	}
}

// CheckOutletLimit middleware checks if tenant can create more outlets
func (l *LimitChecker) CheckOutletLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != "POST" {
			c.Next()
			return
		}

		tenantID := c.GetString("tenant_id")

		var subscription database.Subscription
		if err := l.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
			c.Next()
			return
		}

		if subscription.MaxOutlets == 0 {
			c.Next()
			return
		}

		var outletCount int64
		l.db.Model(&database.Outlet{}).Where("tenant_id = ?", tenantID).Count(&outletCount)

		if int(outletCount) >= subscription.MaxOutlets {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "Outlet limit reached",
				"message": "Batas outlet tercapai. Upgrade paket untuk menambah outlet.",
				"code":    "LIMIT_OUTLETS",
				"current": outletCount,
				"limit":   subscription.MaxOutlets,
			})
			return
		}

		c.Next()
	}
}
