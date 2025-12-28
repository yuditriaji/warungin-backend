package dashboard

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

type DashboardStats struct {
	TodaySales       float64 `json:"today_sales"`
	TodayTransactions int     `json:"today_transactions"`
	TodayItemsSold   int     `json:"today_items_sold"`
	WeekSales        float64 `json:"week_sales"`
	WeekTransactions int     `json:"week_transactions"`
	MonthSales       float64 `json:"month_sales"`
	MonthTransactions int    `json:"month_transactions"`
	TotalProducts    int     `json:"total_products"`
	LowStockProducts int     `json:"low_stock_products"`
}

type TopProduct struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	TotalQty    int     `json:"total_qty"`
	TotalSales  float64 `json:"total_sales"`
}

// GetStats returns dashboard statistics
func (h *Handler) GetStats(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := todayStart.AddDate(0, 0, -7)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var stats DashboardStats

	// Today's stats
	var todayResult struct {
		Total float64
		Count int
		Items int
	}
	h.db.Model(&database.Transaction{}).
		Select("COALESCE(SUM(total), 0) as total, COUNT(*) as count").
		Where("tenant_id = ? AND created_at >= ? AND status = ?", tenantID, todayStart, "completed").
		Scan(&todayResult)
	stats.TodaySales = todayResult.Total
	stats.TodayTransactions = todayResult.Count

	// Count items sold today
	h.db.Model(&database.TransactionItem{}).
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.status = ?", 
			tenantID, todayStart, "completed").
		Select("COALESCE(SUM(transaction_items.quantity), 0)").
		Scan(&stats.TodayItemsSold)

	// Week stats
	var weekResult struct {
		Total float64
		Count int
	}
	h.db.Model(&database.Transaction{}).
		Select("COALESCE(SUM(total), 0) as total, COUNT(*) as count").
		Where("tenant_id = ? AND created_at >= ? AND status = ?", tenantID, weekStart, "completed").
		Scan(&weekResult)
	stats.WeekSales = weekResult.Total
	stats.WeekTransactions = weekResult.Count

	// Month stats
	var monthResult struct {
		Total float64
		Count int
	}
	h.db.Model(&database.Transaction{}).
		Select("COALESCE(SUM(total), 0) as total, COUNT(*) as count").
		Where("tenant_id = ? AND created_at >= ? AND status = ?", tenantID, monthStart, "completed").
		Scan(&monthResult)
	stats.MonthSales = monthResult.Total
	stats.MonthTransactions = monthResult.Count

	// Product counts
	var totalProducts int64
	h.db.Model(&database.Product{}).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Count(&totalProducts)
	stats.TotalProducts = int(totalProducts)

	var lowStockProducts int64
	h.db.Model(&database.Product{}).
		Where("tenant_id = ? AND is_active = ? AND stock_qty < ?", tenantID, true, 10).
		Count(&lowStockProducts)
	stats.LowStockProducts = int(lowStockProducts)

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// GetTopProducts returns best selling products
func (h *Handler) GetTopProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var topProducts []TopProduct
	h.db.Model(&database.TransactionItem{}).
		Select("transaction_items.product_id, products.name as product_name, SUM(transaction_items.quantity) as total_qty, SUM(transaction_items.subtotal) as total_sales").
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Joins("JOIN products ON transaction_items.product_id = products.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.status = ?", 
			tenantID, monthStart, "completed").
		Group("transaction_items.product_id, products.name").
		Order("total_qty DESC").
		Limit(5).
		Scan(&topProducts)

	c.JSON(http.StatusOK, gin.H{"data": topProducts})
}

// GetRecentTransactions returns latest transactions
func (h *Handler) GetRecentTransactions(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var transactions []database.Transaction
	h.db.Where("tenant_id = ?", tenantID).
		Preload("Items").
		Order("created_at DESC").
		Limit(5).
		Find(&transactions)

	c.JSON(http.StatusOK, gin.H{"data": transactions})
}
