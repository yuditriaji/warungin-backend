package inventory

import (
	"math"
	"net/http"

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

type InventoryItem struct {
	ProductID        uuid.UUID `json:"product_id"`
	ProductName      string    `json:"product_name"`
	SKU              string    `json:"sku"`
	StockQty         int       `json:"stock_qty"`
	UseMaterialStock bool      `json:"use_material_stock"`
	Price            float64   `json:"price"`
	Cost             float64   `json:"cost"`
	StockValue       float64   `json:"stock_value"`
	Status           string    `json:"status"` // ok, low, out
}

type InventorySummary struct {
	TotalProducts   int     `json:"total_products"`
	TotalStockValue float64 `json:"total_stock_value"`
	LowStockCount   int     `json:"low_stock_count"`
	OutOfStockCount int     `json:"out_of_stock_count"`
}

// GetInventory returns inventory status for all products
func (h *Handler) GetInventory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	filter := c.Query("filter") // all, low, out
	outletID := c.Query("outlet_id")

	query := h.db.Where("tenant_id = ? AND is_active = ?", tenantID, true)
	
	// Filter by outlet if specified
	if outletID != "" {
		query = query.Where("outlet_id = ?", outletID)
	}
	
	var products []database.Product
	query.Order("name ASC").Find(&products)

	var items []InventoryItem
	for _, p := range products {
		stockQty := p.StockQty

		// Calculate stock from materials if UseMaterialStock is true
		if p.UseMaterialStock {
			stockQty = h.calculateMaterialStock(p.ID)
		}

		status := "ok"
		if stockQty <= 0 {
			status = "out"
		} else if stockQty < 10 {
			status = "low"
		}

		// Apply filter
		if filter == "low" && status != "low" {
			continue
		}
		if filter == "out" && status != "out" {
			continue
		}

		// Calculate cost for material-driven products
		cost := p.Cost
		if p.UseMaterialStock && cost <= 0 {
			cost = h.calculateMaterialCost(p.ID)
		}

		items = append(items, InventoryItem{
			ProductID:        p.ID,
			ProductName:      p.Name,
			SKU:              p.SKU,
			StockQty:         stockQty,
			UseMaterialStock: p.UseMaterialStock,
			Price:            p.Price,
			Cost:             cost,
			StockValue:       float64(stockQty) * cost,
			Status:           status,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": items})
}

// calculateMaterialStock returns the max units that can be made from available materials
func (h *Handler) calculateMaterialStock(productID uuid.UUID) int {
	var productMaterials []database.ProductMaterial
	h.db.Where("product_id = ?", productID).Preload("Material").Find(&productMaterials)

	if len(productMaterials) == 0 {
		return 0
	}

	availableStock := math.MaxFloat64
	for _, pm := range productMaterials {
		if pm.QuantityUsed <= 0 {
			continue
		}
		
		// Account for conversion rate (recipe_qty × conversion = actual material usage)
		convRate := pm.ConversionRate
		if convRate <= 0 {
			convRate = 1
		}
		actualUsage := pm.QuantityUsed * convRate
		
		canMake := pm.Material.StockQty / actualUsage
		if canMake < availableStock {
			availableStock = canMake
		}
	}

	if availableStock == math.MaxFloat64 {
		return 0
	}
	return int(math.Floor(availableStock))
}

// calculateMaterialCost returns the total cost of materials for one product unit
func (h *Handler) calculateMaterialCost(productID uuid.UUID) float64 {
	var productMaterials []database.ProductMaterial
	h.db.Where("product_id = ?", productID).Preload("Material").Find(&productMaterials)

	var totalCost float64
	for _, pm := range productMaterials {
		convRate := pm.ConversionRate
		if convRate <= 0 {
			convRate = 1
		}
		// Cost = quantity × conversion × unit_price
		totalCost += pm.QuantityUsed * convRate * pm.Material.UnitPrice
	}
	return totalCost
}

// GetSummary returns inventory summary stats
func (h *Handler) GetSummary(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	outletID := c.Query("outlet_id")

	var summary InventorySummary

	// Build base query conditions
	baseCondition := "tenant_id = ? AND is_active = ?"
	baseArgs := []interface{}{tenantID, true}
	
	if outletID != "" {
		baseCondition += " AND outlet_id = ?"
		baseArgs = append(baseArgs, outletID)
	}

	// Total products
	var totalProducts int64
	h.db.Model(&database.Product{}).
		Where(baseCondition, baseArgs...).
		Count(&totalProducts)
	summary.TotalProducts = int(totalProducts)

	// Total stock value
	var stockValue struct {
		Total float64
	}
	h.db.Model(&database.Product{}).
		Select("COALESCE(SUM(stock_qty * cost), 0) as total").
		Where(baseCondition, baseArgs...).
		Scan(&stockValue)
	summary.TotalStockValue = stockValue.Total

	// Low stock count
	var lowStock int64
	h.db.Model(&database.Product{}).
		Where(baseCondition+" AND stock_qty > 0 AND stock_qty < 10", baseArgs...).
		Count(&lowStock)
	summary.LowStockCount = int(lowStock)

	// Out of stock count
	var outOfStock int64
	h.db.Model(&database.Product{}).
		Where(baseCondition+" AND stock_qty <= 0", baseArgs...).
		Count(&outOfStock)
	summary.OutOfStockCount = int(outOfStock)

	c.JSON(http.StatusOK, gin.H{"data": summary})
}

// UpdateStock adjusts product stock
type UpdateStockRequest struct {
	Quantity int    `json:"quantity" binding:"required"` // can be negative
	Note     string `json:"note"`
}

func (h *Handler) UpdateStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	var req UpdateStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product database.Product
	if err := h.db.Where("id = ? AND tenant_id = ?", productID, tenantID).First(&product).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	newQty := product.StockQty + req.Quantity
	if newQty < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Stock cannot go below zero"})
		return
	}

	product.StockQty = newQty
	h.db.Save(&product)

	c.JSON(http.StatusOK, gin.H{"data": product})
}

// GetAlerts returns products that need attention
func (h *Handler) GetAlerts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var lowStock []database.Product
	h.db.Where("tenant_id = ? AND is_active = ? AND stock_qty > 0 AND stock_qty < 10", tenantID, true).
		Order("stock_qty ASC").
		Limit(10).
		Find(&lowStock)

	var outOfStock []database.Product
	h.db.Where("tenant_id = ? AND is_active = ? AND stock_qty <= 0", tenantID, true).
		Order("name ASC").
		Limit(10).
		Find(&outOfStock)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"low_stock":    lowStock,
			"out_of_stock": outOfStock,
		},
	})
}
