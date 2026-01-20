package transaction

import (
	"encoding/json"
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

	// Get user's outlet_id
	var user database.User
	h.db.Where("id = ?", userID).First(&user)

	// Start transaction
	tx := h.db.Begin()

	// Get next order number for today (queue number)
	today := time.Now().Format("2006-01-02")
	var lastOrder database.Transaction
	var orderNumber int = 1
	if err := tx.Where("tenant_id = ? AND DATE(created_at) = ?", tenantID, today).
		Order("order_number DESC").First(&lastOrder).Error; err == nil {
		orderNumber = lastOrder.OrderNumber + 1
	}

	// Calculate totals with per-product tax
	var items []database.TransactionItem
	var subtotal float64
	var totalTax float64

	for _, item := range req.Items {
		var product database.Product
		if err := tx.Where("id = ? AND tenant_id = ?", item.ProductID, tenantID).First(&product).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Product %s not found", item.ProductID)})
			return
		}

		itemSubtotal := product.Price * float64(item.Quantity)
		// Calculate tax per product
		itemTax := itemSubtotal * (product.TaxRate / 100)
		totalTax += itemTax

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

		// Auto-deduct raw materials linked to this product
		var productMaterials []database.ProductMaterial
		tx.Where("product_id = ?", item.ProductID).Find(&productMaterials)
		for _, pm := range productMaterials {
			deduction := pm.QuantityUsed * float64(item.Quantity)
			tx.Model(&database.RawMaterial{}).
				Where("id = ?", pm.MaterialID).
				Update("stock_qty", gorm.Expr("stock_qty - ?", deduction))
		}
	}

	// Use calculated tax if not provided from request
	finalTax := req.Tax
	if finalTax == 0 {
		finalTax = totalTax
	}

	total := subtotal - req.Discount + finalTax
	paymentMethod := req.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = "cash"
	}

	// Generate invoice number
	invoiceNumber := fmt.Sprintf("INV-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano()%10000)

	transaction := database.Transaction{
		TenantID:      tenantID,
		OutletID:      user.OutletID, // Set outlet from user's assigned outlet
		InvoiceNumber: invoiceNumber,
		OrderNumber:   orderNumber,
		UserID:        userID,
		CustomerID:    req.CustomerID,
		Items:         items,
		Subtotal:      subtotal,
		Discount:      req.Discount,
		Tax:           finalTax,
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

type VoidTransactionRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// Void cancels a transaction with audit logging and role-based restrictions
func (h *Handler) Void(c *gin.Context) {
	tenantIDStr := c.GetString("tenant_id")
	tenantID, _ := uuid.Parse(tenantIDStr)
	userIDStr := c.GetString("user_id")
	userID, _ := uuid.Parse(userIDStr)
	transactionID := c.Param("id")

	var req VoidTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Alasan pembatalan wajib diisi"})
		return
	}

	// Get the user's role
	var user database.User
	if err := h.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	// Get the transaction
	var transaction database.Transaction
	if err := h.db.Where("id = ? AND tenant_id = ?", transactionID, tenantID).
		Preload("Items").First(&transaction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Transaksi tidak ditemukan"})
		return
	}

	// Check if already voided
	if transaction.Status == "voided" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Transaksi sudah dibatalkan"})
		return
	}

	// Time-based role restrictions
	timeSinceCreation := time.Since(transaction.CreatedAt)
	fiveMinutes := 5 * time.Minute
	oneDay := 24 * time.Hour

	switch user.Role {
	case "cashier":
		if timeSinceCreation > fiveMinutes {
			c.JSON(http.StatusForbidden, gin.H{"error": "Kasir hanya dapat membatalkan transaksi dalam 5 menit pertama. Hubungi manager."})
			return
		}
	case "manager":
		if timeSinceCreation > oneDay {
			c.JSON(http.StatusForbidden, gin.H{"error": "Manager hanya dapat membatalkan transaksi dalam 24 jam. Hubungi owner."})
			return
		}
	case "owner":
		// Owner can void any transaction
	default:
		c.JSON(http.StatusForbidden, gin.H{"error": "Tidak memiliki izin untuk membatalkan transaksi"})
		return
	}

	// Start database transaction
	tx := h.db.Begin()

	// Store old values as JSON for audit
	oldValuesJSON, _ := json.Marshal(map[string]interface{}{
		"status":    transaction.Status,
		"total":     transaction.Total,
		"subtotal":  transaction.Subtotal,
		"discount":  transaction.Discount,
		"tax":       transaction.Tax,
		"items":     transaction.Items,
	})

	// Void the transaction
	if err := tx.Model(&transaction).Update("status", "voided").Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membatalkan transaksi"})
		return
	}

	// Restore stock for each item
	for _, item := range transaction.Items {
		if err := tx.Model(&database.Product{}).
			Where("id = ?", item.ProductID).
			Update("stock_qty", gorm.Expr("stock_qty + ?", item.Quantity)).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengembalikan stok"})
			return
		}

		// Restore raw materials
		var productMaterials []database.ProductMaterial
		h.db.Where("product_id = ?", item.ProductID).Find(&productMaterials)
		for _, pm := range productMaterials {
			restoration := pm.QuantityUsed * float64(item.Quantity)
			tx.Model(&database.RawMaterial{}).
				Where("id = ?", pm.MaterialID).
				Update("stock_qty", gorm.Expr("stock_qty + ?", restoration))
		}
	}

	// Create audit log entry
	auditLog := database.TransactionAuditLog{
		TenantID:      tenantID,
		TransactionID: transaction.ID,
		Action:        "void",
		Reason:        req.Reason,
		OldValues:     string(oldValuesJSON),
		NewValues:     `{"status": "voided"}`,
		UserID:        userID,
		IPAddress:     c.ClientIP(),
	}
	if err := tx.Create(&auditLog).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mencatat audit log"})
		return
	}

	tx.Commit()

	c.JSON(http.StatusOK, gin.H{
		"message": "Transaksi berhasil dibatalkan",
		"data":    transaction,
	})
}
