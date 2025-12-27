package transaction

import (
	"fmt"
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

type TransactionItemRequest struct {
	ProductID uuid.UUID `json:"product_id" binding:"required"`
	Quantity  int       `json:"quantity" binding:"required,min=1"`
}

type CreateTransactionRequest struct {
	CustomerID    *uuid.UUID               `json:"customer_id"`
	Items         []TransactionItemRequest `json:"items" binding:"required,min=1"`
	Discount      float64                  `json:"discount"`
	Tax           float64                  `json:"tax"`
	PaymentMethod string                   `json:"payment_method"`
}

// List returns all transactions for the tenant
func (h *Handler) List(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var transactions []database.Transaction
	if err := h.db.Where("tenant_id = ?", tenantID).
		Preload("Items").
		Preload("Items.Product").
		Preload("Customer").
		Order("created_at DESC").
		Find(&transactions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch transactions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": transactions})
}

// Create processes a new sale transaction
func (h *Handler) Create(c *gin.Context) {
	var req CreateTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantIDStr := c.GetString("tenant_id")
	tenantID, _ := uuid.Parse(tenantIDStr)
	userIDStr := c.GetString("user_id")
	userID, _ := uuid.Parse(userIDStr)

	// Start transaction
	tx := h.db.Begin()

	// Calculate totals and build items
	var items []database.TransactionItem
	var subtotal float64

	for _, item := range req.Items {
		var product database.Product
		if err := tx.Where("id = ? AND tenant_id = ?", item.ProductID, tenantID).First(&product).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Product %s not found", item.ProductID)})
			return
		}

		itemSubtotal := product.Price * float64(item.Quantity)
		items = append(items, database.TransactionItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			UnitPrice: product.Price,
			Subtotal:  itemSubtotal,
		})
		subtotal += itemSubtotal

		// Reduce stock
		if err := tx.Model(&product).Update("stock_qty", gorm.Expr("stock_qty - ?", item.Quantity)).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
			return
		}
	}

	total := subtotal - req.Discount + req.Tax
	paymentMethod := req.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = "cash"
	}

	// Generate invoice number
	invoiceNumber := fmt.Sprintf("INV-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano()%10000)

	transaction := database.Transaction{
		TenantID:      tenantID,
		InvoiceNumber: invoiceNumber,
		UserID:        userID,
		CustomerID:    req.CustomerID,
		Items:         items,
		Subtotal:      subtotal,
		Discount:      req.Discount,
		Tax:           req.Tax,
		Total:         total,
		Status:        "completed",
		PaymentMethod: paymentMethod,
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create transaction"})
		return
	}

	tx.Commit()

	// Reload with associations
	h.db.Preload("Items").Preload("Items.Product").Preload("Customer").First(&transaction, transaction.ID)

	c.JSON(http.StatusCreated, gin.H{"data": transaction})
}

// Get returns a single transaction
func (h *Handler) Get(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	transactionID := c.Param("id")

	var transaction database.Transaction
	if err := h.db.Where("id = ? AND tenant_id = ?", transactionID, tenantID).
		Preload("Items").
		Preload("Items.Product").
		Preload("Customer").
		First(&transaction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": transaction})
}
