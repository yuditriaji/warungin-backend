package material

import (
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

type CreateMaterialInput struct {
	Name      string  `json:"name" binding:"required"`
	Unit      string  `json:"unit" binding:"required"`
	UnitPrice float64 `json:"unit_price"`
	StockQty  float64 `json:"stock_qty"`
	Supplier  string  `json:"supplier"`
}

// List returns all raw materials for tenant
func (h *Handler) List(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var materials []database.RawMaterial
	if err := h.db.Where("tenant_id = ?", tenantID).
		Order("name ASC").
		Find(&materials).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": materials})
}

// Create adds a new raw material
func (h *Handler) Create(c *gin.Context) {
	var input CreateMaterialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	tenantUUID, _ := uuid.Parse(tenantID)

	material := database.RawMaterial{
		TenantID:  tenantUUID,
		Name:      input.Name,
		Unit:      input.Unit,
		UnitPrice: input.UnitPrice,
		StockQty:  input.StockQty,
		Supplier:  input.Supplier,
	}

	if err := h.db.Create(&material).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": material})
}

// Get returns a single material
func (h *Handler) Get(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var material database.RawMaterial
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&material).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Material not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": material})
}

// Update modifies a raw material
func (h *Handler) Update(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var material database.RawMaterial
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&material).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Material not found"})
		return
	}

	var input CreateMaterialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	material.Name = input.Name
	material.Unit = input.Unit
	material.UnitPrice = input.UnitPrice
	material.StockQty = input.StockQty
	material.Supplier = input.Supplier

	h.db.Save(&material)

	c.JSON(http.StatusOK, gin.H{"data": material})
}

// Delete removes a raw material
func (h *Handler) Delete(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	result := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&database.RawMaterial{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Material not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Material deleted"})
}

// UpdateStock adjusts material stock
func (h *Handler) UpdateStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var input struct {
		Adjustment float64 `json:"adjustment" binding:"required"`
		Reason     string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var material database.RawMaterial
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&material).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Material not found"})
		return
	}

	material.StockQty += input.Adjustment
	if material.StockQty < 0 {
		material.StockQty = 0
	}
	h.db.Save(&material)

	c.JSON(http.StatusOK, gin.H{"data": material})
}

// GetAlerts returns materials with low stock
func (h *Handler) GetAlerts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// Low stock threshold: less than 10 units
	var lowStock []database.RawMaterial
	h.db.Where("tenant_id = ? AND stock_qty > 0 AND stock_qty < 10", tenantID).
		Order("stock_qty ASC").
		Find(&lowStock)

	// Out of stock
	var outOfStock []database.RawMaterial
	h.db.Where("tenant_id = ? AND stock_qty <= 0", tenantID).
		Find(&outOfStock)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"low_stock":    lowStock,
			"out_of_stock": outOfStock,
		},
	})
}

// === Product-Material Linkage ===

type LinkMaterialInput struct {
	ProductID    string  `json:"product_id" binding:"required"`
	MaterialID   string  `json:"material_id" binding:"required"`
	QuantityUsed float64 `json:"quantity_used" binding:"required"`
}

// GetProductMaterials returns materials linked to a product
func (h *Handler) GetProductMaterials(c *gin.Context) {
	productID := c.Param("product_id")

	var links []database.ProductMaterial
	if err := h.db.Preload("Material").
		Where("product_id = ?", productID).
		Find(&links).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate total material cost
	var totalCost float64
	for _, link := range links {
		totalCost += link.Material.UnitPrice * link.QuantityUsed
	}

	c.JSON(http.StatusOK, gin.H{
		"data":          links,
		"material_cost": totalCost,
	})
}

// LinkMaterial adds a material to a product
func (h *Handler) LinkMaterial(c *gin.Context) {
	var input LinkMaterialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	productUUID, _ := uuid.Parse(input.ProductID)
	materialUUID, _ := uuid.Parse(input.MaterialID)

	// Check if already linked
	var existing database.ProductMaterial
	if h.db.Where("product_id = ? AND material_id = ?", productUUID, materialUUID).
		First(&existing).Error == nil {
		// Update quantity
		existing.QuantityUsed = input.QuantityUsed
		h.db.Save(&existing)
		c.JSON(http.StatusOK, gin.H{"data": existing})
		return
	}

	link := database.ProductMaterial{
		ProductID:    productUUID,
		MaterialID:   materialUUID,
		QuantityUsed: input.QuantityUsed,
	}

	if err := h.db.Create(&link).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": link})
}

// UnlinkMaterial removes a material from a product
func (h *Handler) UnlinkMaterial(c *gin.Context) {
	productID := c.Param("product_id")
	materialID := c.Param("material_id")

	result := h.db.Where("product_id = ? AND material_id = ?", productID, materialID).
		Delete(&database.ProductMaterial{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Link not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Material unlinked"})
}

// CalculateProductCost calculates cost based on materials
func (h *Handler) CalculateProductCost(c *gin.Context) {
	productID := c.Param("product_id")

	var links []database.ProductMaterial
	h.db.Preload("Material").Where("product_id = ?", productID).Find(&links)

	var totalCost float64
	var breakdown []gin.H
	for _, link := range links {
		cost := link.Material.UnitPrice * link.QuantityUsed
		totalCost += cost
		breakdown = append(breakdown, gin.H{
			"material":     link.Material.Name,
			"quantity":     link.QuantityUsed,
			"unit":         link.Material.Unit,
			"unit_price":   link.Material.UnitPrice,
			"cost":         cost,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":  productID,
		"total_cost":  totalCost,
		"breakdown":   breakdown,
	})
}
