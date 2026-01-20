package customer

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/activitylog"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

type Handler struct {
	db     *gorm.DB
	logger *activitylog.Logger
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{
		db:     db,
		logger: activitylog.NewLogger(db),
	}
}

type CreateCustomerRequest struct {
	Name    string `json:"name" binding:"required"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Address string `json:"address"`
}

// List returns all customers for the tenant
func (h *Handler) List(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	search := c.Query("search")

	var customers []database.Customer
	query := h.db.Where("tenant_id = ?", tenantID)
	
	if search != "" {
		query = query.Where("name ILIKE ? OR phone ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	
	if err := query.Order("name ASC").Find(&customers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch customers"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": customers})
}

// Create adds a new customer
func (h *Handler) Create(c *gin.Context) {
	var req CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantIDStr := c.GetString("tenant_id")
	tenantID, _ := uuid.Parse(tenantIDStr)

	customer := database.Customer{
		TenantID: tenantID,
		Name:     req.Name,
		Phone:    req.Phone,
		Email:    req.Email,
		Address:  req.Address,
	}

	if err := h.db.Create(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create customer"})
		return
	}

	// Log activity
	h.logger.LogCreate(c, "customer", customer.ID, map[string]interface{}{
		"name":  customer.Name,
		"phone": customer.Phone,
		"email": customer.Email,
	})

	c.JSON(http.StatusCreated, gin.H{"data": customer})
}

// Get returns a single customer
func (h *Handler) Get(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	customerID := c.Param("id")

	var customer database.Customer
	if err := h.db.Where("id = ? AND tenant_id = ?", customerID, tenantID).First(&customer).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Customer not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": customer})
}

// Update modifies a customer
func (h *Handler) Update(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	customerID := c.Param("id")

	var customer database.Customer
	if err := h.db.Where("id = ? AND tenant_id = ?", customerID, tenantID).First(&customer).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Customer not found"})
		return
	}

	// Store old values for logging
	oldValues := map[string]interface{}{
		"name":    customer.Name,
		"phone":   customer.Phone,
		"email":   customer.Email,
		"address": customer.Address,
	}

	var req CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	customer.Name = req.Name
	customer.Phone = req.Phone
	customer.Email = req.Email
	customer.Address = req.Address

	if err := h.db.Save(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update customer"})
		return
	}

	// Log activity with old and new values
	h.logger.LogUpdate(c, "customer", customer.ID, oldValues, map[string]interface{}{
		"name":    customer.Name,
		"phone":   customer.Phone,
		"email":   customer.Email,
		"address": customer.Address,
	})

	c.JSON(http.StatusOK, gin.H{"data": customer})
}

// Delete soft-deletes a customer
func (h *Handler) Delete(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	customerID := c.Param("id")

	// Get customer before delete for logging
	var customer database.Customer
	if err := h.db.Where("id = ? AND tenant_id = ?", customerID, tenantID).First(&customer).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Customer not found"})
		return
	}

	if err := h.db.Delete(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete customer"})
		return
	}

	// Log activity
	h.logger.LogDelete(c, "customer", customer.ID, map[string]interface{}{
		"name":  customer.Name,
		"phone": customer.Phone,
		"email": customer.Email,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Customer deleted"})
}

// GetStats returns customer purchase statistics
func (h *Handler) GetStats(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	customerID := c.Param("id")

	var stats struct {
		TotalTransactions int64   `json:"total_transactions"`
		TotalSpent        float64 `json:"total_spent"`
	}

	h.db.Model(&database.Transaction{}).
		Select("COUNT(*) as total_transactions, COALESCE(SUM(total), 0) as total_spent").
		Where("tenant_id = ? AND customer_id = ? AND status = ?", tenantID, customerID, "completed").
		Scan(&stats)

	c.JSON(http.StatusOK, gin.H{"data": stats})
}
