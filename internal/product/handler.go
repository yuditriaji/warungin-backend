package product

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

type CreateProductRequest struct {
	Name       string     `json:"name" binding:"required"`
	SKU        string     `json:"sku"`
	Price      float64    `json:"price" binding:"required"`
	Cost       float64    `json:"cost"`
	StockQty   int        `json:"stock_qty"`
	CategoryID *uuid.UUID `json:"category_id"`
	ImageURL   string     `json:"image_url"`
}

// List returns all products for the tenant
func (h *Handler) List(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var products []database.Product
	if err := h.db.Where("tenant_id = ?", tenantID).
		Preload("Category").
		Find(&products).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch products"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": products})
}

// Create adds a new product
func (h *Handler) Create(c *gin.Context) {
	var req CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantIDStr := c.GetString("tenant_id")
	tenantID, _ := uuid.Parse(tenantIDStr)

	product := database.Product{
		TenantID:   tenantID,
		Name:       req.Name,
		SKU:        req.SKU,
		Price:      req.Price,
		Cost:       req.Cost,
		StockQty:   req.StockQty,
		CategoryID: req.CategoryID,
		ImageURL:   req.ImageURL,
		IsActive:   true,
	}

	if err := h.db.Create(&product).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create product"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": product})
}

// Get returns a single product
func (h *Handler) Get(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	var product database.Product
	if err := h.db.Where("id = ? AND tenant_id = ?", productID, tenantID).
		Preload("Category").
		First(&product).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": product})
}

// Update modifies a product
func (h *Handler) Update(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	var product database.Product
	if err := h.db.Where("id = ? AND tenant_id = ?", productID, tenantID).First(&product).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	var req CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	product.Name = req.Name
	product.SKU = req.SKU
	product.Price = req.Price
	product.Cost = req.Cost
	product.StockQty = req.StockQty
	product.CategoryID = req.CategoryID
	product.ImageURL = req.ImageURL

	if err := h.db.Save(&product).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": product})
}

// Delete soft-deletes a product
func (h *Handler) Delete(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	if err := h.db.Where("id = ? AND tenant_id = ?", productID, tenantID).Delete(&database.Product{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Product deleted"})
}
