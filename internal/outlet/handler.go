package outlet

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

type CreateOutletInput struct {
	Name    string `json:"name" binding:"required"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

// List returns all outlets for tenant
func (h *Handler) List(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var outlets []database.Outlet
	if err := h.db.Where("tenant_id = ?", tenantID).
		Order("created_at ASC").
		Find(&outlets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": outlets})
}

// Create adds a new outlet
func (h *Handler) Create(c *gin.Context) {
	var input CreateOutletInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	tenantUUID, _ := uuid.Parse(tenantID)

	// Check subscription limit
	var outletCount int64
	h.db.Model(&database.Outlet{}).Where("tenant_id = ?", tenantID).Count(&outletCount)

	var sub database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&sub)

	maxOutlets := getMaxOutlets(sub.Plan)
	if int(outletCount) >= maxOutlets {
		c.JSON(http.StatusForbidden, gin.H{
			"error":       "Outlet limit reached",
			"max_outlets": maxOutlets,
			"current":     outletCount,
		})
		return
	}

	outlet := database.Outlet{
		TenantID: tenantUUID,
		Name:     input.Name,
		Address:  input.Address,
		Phone:    input.Phone,
		IsActive: true,
	}

	if err := h.db.Create(&outlet).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": outlet})
}

// Get returns a single outlet
func (h *Handler) Get(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var outlet database.Outlet
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&outlet).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Outlet not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": outlet})
}

// Update modifies an outlet
func (h *Handler) Update(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var outlet database.Outlet
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&outlet).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Outlet not found"})
		return
	}

	var input CreateOutletInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	outlet.Name = input.Name
	outlet.Address = input.Address
	outlet.Phone = input.Phone
	h.db.Save(&outlet)

	c.JSON(http.StatusOK, gin.H{"data": outlet})
}

// Delete removes an outlet
func (h *Handler) Delete(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	// Check if this is the only outlet
	var count int64
	h.db.Model(&database.Outlet{}).Where("tenant_id = ?", tenantID).Count(&count)
	if count <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete the only outlet"})
		return
	}

	result := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&database.Outlet{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Outlet not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Outlet deleted"})
}

// GetStats returns stats for a specific outlet
func (h *Handler) GetStats(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	outletID := c.Param("id")

	// Today's sales for this outlet
	var todaySales float64
	h.db.Model(&database.Transaction{}).
		Where("tenant_id = ? AND outlet_id = ? AND DATE(created_at) = CURRENT_DATE", tenantID, outletID).
		Select("COALESCE(SUM(total), 0)").
		Scan(&todaySales)

	// Transaction count today
	var todayTxCount int64
	h.db.Model(&database.Transaction{}).
		Where("tenant_id = ? AND outlet_id = ? AND DATE(created_at) = CURRENT_DATE", tenantID, outletID).
		Count(&todayTxCount)

	// This month's sales
	var monthSales float64
	h.db.Model(&database.Transaction{}).
		Where("tenant_id = ? AND outlet_id = ? AND DATE_TRUNC('month', created_at) = DATE_TRUNC('month', CURRENT_DATE)", tenantID, outletID).
		Select("COALESCE(SUM(total), 0)").
		Scan(&monthSales)

	c.JSON(http.StatusOK, gin.H{
		"outlet_id":       outletID,
		"today_sales":     todaySales,
		"today_tx_count":  todayTxCount,
		"month_sales":     monthSales,
	})
}

// SwitchOutlet updates the user's current outlet
func (h *Handler) SwitchOutlet(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	userID := c.GetString("user_id")
	outletID := c.Param("id")

	// Verify outlet belongs to tenant
	var outlet database.Outlet
	if err := h.db.Where("id = ? AND tenant_id = ?", outletID, tenantID).
		First(&outlet).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Outlet not found"})
		return
	}

	// Update user's outlet
	outletUUID, _ := uuid.Parse(outletID)
	h.db.Model(&database.User{}).
		Where("id = ?", userID).
		Update("outlet_id", outletUUID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Switched to outlet: " + outlet.Name,
		"outlet":  outlet,
	})
}

func getMaxOutlets(plan string) int {
	switch plan {
	case "gratis":
		return 1
	case "pemula":
		return 1
	case "bisnis":
		return 3
	case "enterprise":
		return 999 // Unlimited
	default:
		return 1
	}
}
