package reports

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

type SalesReportRequest struct {
	StartDate string `form:"start_date"` // Format: 2024-01-01
	EndDate   string `form:"end_date"`   // Format: 2024-01-31
	OutletID  string `form:"outlet_id"`  // Optional outlet filter
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
	totalsQuery := h.db.Model(&database.Transaction{}).
		Select("COALESCE(SUM(total), 0) as sales, COUNT(*) as transactions").
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ? AND status = ?", 
			tenantID, startDate, endDate, "completed")
	
	// Add outlet filter if provided
	if req.OutletID != "" {
		totalsQuery = totalsQuery.Where("outlet_id = ?", req.OutletID)
	}
	totalsQuery.Scan(&totals)
	
	report.TotalSales = totals.Sales
	report.TotalTransactions = int(totals.Transactions)
	if report.TotalTransactions > 0 {
		report.AveragePerTx = report.TotalSales / float64(report.TotalTransactions)
	}

	// Get items sold count
	var itemCount int64
	itemsQuery := h.db.Model(&database.TransactionItem{}).
		Select("COALESCE(SUM(transaction_items.quantity), 0)").
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.created_at <= ? AND transactions.status = ?",
			tenantID, startDate, endDate, "completed")
	
	if req.OutletID != "" {
		itemsQuery = itemsQuery.Where("transactions.outlet_id = ?", req.OutletID)
	}
	itemsQuery.Scan(&itemCount)
	report.TotalItemsSold = int(itemCount)

	// Calculate total cost by iterating through transaction items
	// This properly handles material-driven products
	report.TotalCost = h.calculateTotalCOGS(tenantID, startDate, endDate, req.OutletID)
	report.GrossProfit = report.TotalSales - report.TotalCost

	// Get daily breakdown
	dailyQuery := h.db.Model(&database.Transaction{}).
		Select("DATE(created_at) as date, COALESCE(SUM(total), 0) as sales, COUNT(*) as transactions").
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ? AND status = ?",
			tenantID, startDate, endDate, "completed")
	
	if req.OutletID != "" {
		dailyQuery = dailyQuery.Where("outlet_id = ?", req.OutletID)
	}
	
	rows, _ := dailyQuery.Group("DATE(created_at)").Order("date ASC").Rows()
	
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

// calculateTotalCOGS calculates total cost of goods sold, including material costs for material-driven products
func (h *Handler) calculateTotalCOGS(tenantID string, startDate, endDate time.Time, outletID string) float64 {
	// Get all transaction items in the period
	type ItemWithProduct struct {
		ProductID        uuid.UUID
		Quantity         int
		Cost             float64
		UseMaterialStock bool
	}
	
	var items []ItemWithProduct
	query := h.db.Model(&database.TransactionItem{}).
		Select("transaction_items.product_id, transaction_items.quantity, products.cost, products.use_material_stock").
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Joins("JOIN products ON transaction_items.product_id = products.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.created_at <= ? AND transactions.status = ?",
			tenantID, startDate, endDate, "completed")
	
	if outletID != "" {
		query = query.Where("transactions.outlet_id = ?", outletID)
	}
	query.Scan(&items)
	
	var totalCost float64
	for _, item := range items {
		var unitCost float64
		
		if item.UseMaterialStock && item.Cost <= 0 {
			// Calculate cost from materials
			unitCost = h.calculateMaterialCost(item.ProductID)
		} else {
			unitCost = item.Cost
		}
		
		totalCost += unitCost * float64(item.Quantity)
	}
	
	return totalCost
}

// calculateMaterialCost calculates the cost of one unit of a product based on its raw materials
func (h *Handler) calculateMaterialCost(productID uuid.UUID) float64 {
	var productMaterials []database.ProductMaterial
	h.db.Where("product_id = ?", productID).Preload("Material").Find(&productMaterials)

	var totalCost float64
	for _, pm := range productMaterials {
		convRate := pm.ConversionRate
		if convRate <= 0 {
			convRate = 1
		}
		// Cost = quantity_used × conversion_rate × unit_price
		totalCost += pm.QuantityUsed * convRate * pm.Material.UnitPrice
	}
	return totalCost
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

	// Get items grouped by product
	type ProductItem struct {
		ProductID        uuid.UUID
		ProductName      string
		TotalQty         int
		TotalSales       float64
		Cost             float64
		UseMaterialStock bool
	}
	
	var items []ProductItem
	productQuery := h.db.Model(&database.TransactionItem{}).
		Select(`
			transaction_items.product_id, 
			products.name as product_name, 
			SUM(transaction_items.quantity) as total_qty, 
			SUM(transaction_items.subtotal) as total_sales,
			products.cost,
			products.use_material_stock
		`).
		Joins("JOIN transactions ON transaction_items.transaction_id = transactions.id").
		Joins("JOIN products ON transaction_items.product_id = products.id").
		Where("transactions.tenant_id = ? AND transactions.created_at >= ? AND transactions.created_at <= ? AND transactions.status = ?",
			tenantID, startDate, endDate, "completed")
	
	if req.OutletID != "" {
		productQuery = productQuery.Where("transactions.outlet_id = ?", req.OutletID)
	}
	
	productQuery.Group("transaction_items.product_id, products.name, products.cost, products.use_material_stock").
		Order("total_sales DESC").
		Scan(&items)

	// Calculate proper cost for each product
	var products []ProductSalesReport
	for _, item := range items {
		var unitCost float64
		if item.UseMaterialStock && item.Cost <= 0 {
			unitCost = h.calculateMaterialCost(item.ProductID)
		} else {
			unitCost = item.Cost
		}
		
		totalCost := unitCost * float64(item.TotalQty)
		
		products = append(products, ProductSalesReport{
			ProductID:   item.ProductID.String(),
			ProductName: item.ProductName,
			TotalQty:    item.TotalQty,
			TotalSales:  item.TotalSales,
			TotalCost:   totalCost,
			Profit:      item.TotalSales - totalCost,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": products})
}

