package reports

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

type SalesReportRequest struct {
	StartDate string `form:"start_date"` // Format: 2024-01-01
	EndDate   string `form:"end_date"`   // Format: 2024-01-31
}

type DailySales struct {
	Date         string  `json:"date"`
	Sales        float64 `json:"sales"`
	Transactions int     `json:"transactions"`
	ItemsSold    int     `json:"items_sold"`
}

type SalesReport struct {
	StartDate       string       `json:"start_date"`
	EndDate         string       `json:"end_date"`
	TotalSales      float64      `json:"total_sales"`
	TotalCost       float64      `json:"total_cost"`
	GrossProfit     float64      `json:"gross_profit"`
	TotalTransactions int        `json:"total_transactions"`
	TotalItemsSold  int          `json:"total_items_sold"`
	AveragePerTx    float64      `json:"average_per_tx"`
	DailySales      []DailySales `json:"daily_sales"`
}

// GetSalesReport returns sales report for date range
func (h *Handler) GetSalesReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req SalesReportRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Default to current month if no dates provided
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	endDate := time.Date(now.Year(), now.Month()+1, 0, 23, 59, 59, 0, now.Location())

	if req.StartDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.StartDate); err == nil {
			startDate = parsed
		}
	}
	if req.EndDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.EndDate); err == nil {
			endDate = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, parsed.Location())
		}
	}

	var report SalesReport
	report.StartDate = startDate.Format("2006-01-02")
	report.EndDate = endDate.Format("2006-01-02")

	// Get totals
	var totals struct {
		Sales        float64
		Transactions int64
	}
	h.db.Model(&database.Transaction{}).
		Select("COALESCE(SUM(total), 0) as sales, COUNT(*) as transactions").
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ? AND status = ?", 
			tenantID, startDate, endDate, "completed").
		Scan(&totals)
	
	report.TotalSales = totals.Sales
	report.TotalTransactions = int(totals.Transactions)
	if report.TotalTransactions > 0 {
		report.AveragePerTx = report.TotalSales / float64(report.TotalTransactions)
	}

	// Get items sold and cost
	var itemStats struct {
		ItemsSold int64
		TotalCost float64
	}
	h.db.Model(&database.TransactionItem{}).
		Select("COALESCE(SUM(transaction_items.quantity), 0) as items_sold, COALESCE(SUM(products.cost * transaction_items.quantity), 0) as total_cost").
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Joins("JOIN products ON transaction_items.product_id = products.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.created_at <= ? AND transactions.status = ?",
			tenantID, startDate, endDate, "completed").
		Scan(&itemStats)
	
	report.TotalItemsSold = int(itemStats.ItemsSold)
	report.TotalCost = itemStats.TotalCost
	report.GrossProfit = report.TotalSales - report.TotalCost

	// Get daily breakdown
	rows, _ := h.db.Model(&database.Transaction{}).
		Select("DATE(created_at) as date, COALESCE(SUM(total), 0) as sales, COUNT(*) as transactions").
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ? AND status = ?",
			tenantID, startDate, endDate, "completed").
		Group("DATE(created_at)").
		Order("date ASC").
		Rows()
	
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var daily DailySales
			rows.Scan(&daily.Date, &daily.Sales, &daily.Transactions)
			report.DailySales = append(report.DailySales, daily)
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": report})
}

type ProductSalesReport struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	TotalQty    int     `json:"total_qty"`
	TotalSales  float64 `json:"total_sales"`
	TotalCost   float64 `json:"total_cost"`
	Profit      float64 `json:"profit"`
}

// GetProductSalesReport returns sales by product
func (h *Handler) GetProductSalesReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req SalesReportRequest
	c.ShouldBindQuery(&req)

	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	endDate := time.Date(now.Year(), now.Month()+1, 0, 23, 59, 59, 0, now.Location())

	if req.StartDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.StartDate); err == nil {
			startDate = parsed
		}
	}
	if req.EndDate != "" {
		if parsed, err := time.Parse("2006-01-02", req.EndDate); err == nil {
			endDate = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, parsed.Location())
		}
	}

	var products []ProductSalesReport
	h.db.Model(&database.TransactionItem{}).
		Select(`
			transaction_items.product_id, 
			products.name as product_name, 
			SUM(transaction_items.quantity) as total_qty, 
			SUM(transaction_items.subtotal) as total_sales,
			SUM(products.cost * transaction_items.quantity) as total_cost,
			SUM(transaction_items.subtotal) - SUM(products.cost * transaction_items.quantity) as profit
		`).
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Joins("JOIN products ON transaction_items.product_id = products.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.created_at <= ? AND transactions.status = ?",
			tenantID, startDate, endDate, "completed").
		Group("transaction_items.product_id, products.name").
		Order("total_sales DESC").
		Scan(&products)

	c.JSON(http.StatusOK, gin.H{"data": products})
}
