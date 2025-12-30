package user

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

type CreateStaffInput struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role" binding:"required,oneof=manager cashier"`
	OutletID string `json:"outlet_id"` // Optional
}

type UpdateStaffInput struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	OutletID string `json:"outlet_id"`
	IsActive *bool  `json:"is_active"`
}

// ListStaff returns all staff members for tenant
func (h *Handler) ListStaff(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var staff []database.User
	if err := h.db.Preload("Outlet").
		Where("tenant_id = ? AND role != 'owner'", tenantID).
		Order("created_at DESC").
		Find(&staff).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": staff})
}

// CreateStaff adds a new staff member (Owner/Manager only)
func (h *Handler) CreateStaff(c *gin.Context) {
	userRole := c.GetString("role")
	if userRole != "owner" && userRole != "manager" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owner or manager can add staff"})
		return
	}

	var input CreateStaffInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	tenantUUID, _ := uuid.Parse(tenantID)

	// Check subscription limit (Staff Accounts)
	var staffCount int64
	h.db.Model(&database.User{}).Where("tenant_id = ? AND role != 'owner'", tenantID).Count(&staffCount) // Don't count owner

	var sub database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&sub)

	maxUsers := getMaxUsers(sub.Plan)
	// Owner counts as 1, so additional staff = maxUsers - 1 (owner)
	maxStaff := maxUsers - 1
	if sub.Plan == "pemula" {
		maxStaff = 2
	} else if sub.Plan == "bisnis" {
		maxStaff = 9
	}

	if int(staffCount) >= maxStaff && maxUsers != 999 {
		c.JSON(http.StatusForbidden, gin.H{
			"error":     "Staff limit reached",
			"max_staff": maxStaff,
			"current":   staffCount,
		})
		return
	}

	// Check if email exists
	var existing database.User
	if h.db.Where("email = ?", input.Email).First(&existing).Error == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email already registered"})
		return
	}

	// Hash password
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)

	var outletID *uuid.UUID
	if input.OutletID != "" {
		uuidVal, _ := uuid.Parse(input.OutletID)
		outletID = &uuidVal
	}

	staff := database.User{
		TenantID:     tenantUUID,
		Email:        input.Email,
		PasswordHash: string(hashedPassword),
		Name:         input.Name,
		Role:         input.Role,
		OutletID:     outletID,
		IsActive:     true,
	}

	if err := h.db.Create(&staff).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Log activity
	h.logActivity(c, "create_staff", "user", staff.ID, "Created staff "+staff.Name)

	c.JSON(http.StatusCreated, gin.H{"data": staff})
}

// UpdateStaff modifies staff details
func (h *Handler) UpdateStaff(c *gin.Context) {
	userRole := c.GetString("role")
	if userRole != "owner" && userRole != "manager" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Permission denied"})
		return
	}

	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var staff database.User
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}

	if staff.Role == "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot edit owner account"})
		return
	}

	var input UpdateStaffInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name != "" {
		staff.Name = input.Name
	}
	if input.Role != "" {
		staff.Role = input.Role
	}
	if input.OutletID != "" {
		if input.OutletID == "null" {
			staff.OutletID = nil
		} else {
			uuidVal, _ := uuid.Parse(input.OutletID)
			staff.OutletID = &uuidVal
		}
	}
	if input.IsActive != nil {
		staff.IsActive = *input.IsActive
	}

	h.db.Save(&staff)
	h.logActivity(c, "update_staff", "user", staff.ID, "Updated staff "+staff.Name)

	c.JSON(http.StatusOK, gin.H{"data": staff})
}

// DeleteStaff removes a staff member
func (h *Handler) DeleteStaff(c *gin.Context) {
	userRole := c.GetString("role")
	if userRole != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owner can delete staff"})
		return
	}

	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var staff database.User
	if err := h.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&staff).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Staff not found"})
		return
	}

	if staff.Role == "owner" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete owner account"})
		return
	}

	h.db.Delete(&staff)
	h.logActivity(c, "delete_staff", "user", staff.ID, "Deleted staff "+staff.Name)

	c.JSON(http.StatusOK, gin.H{"message": "Staff deleted"})
}

// GetActivityLogs retrieves logs
func (h *Handler) GetActivityLogs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var logs []database.ActivityLog
	if err := h.db.Preload("User").
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Limit(100).
		Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": logs})
}

func (h *Handler) logActivity(c *gin.Context, action, entityType string, entityID uuid.UUID, details string) {
	tenantID := c.GetString("tenant_id")
	userID := c.GetString("user_id")
	outletID := c.GetString("outlet_id")

	tenantUUID, _ := uuid.Parse(tenantID)
	userUUID, _ := uuid.Parse(userID)
	var outletUUID *uuid.UUID
	if outletID != "" {
		val, _ := uuid.Parse(outletID)
		outletUUID = &val
	}

	log := database.ActivityLog{
		TenantID:   tenantUUID,
		UserID:     userUUID,
		OutletID:   outletUUID,
		Action:     action,
		EntityType: entityType,
		EntityID:   &entityID,
		Details:    details,
		IPAddress:  c.ClientIP(),
	}
	h.db.Create(&log)
}

func getMaxUsers(plan string) int {
	switch plan {
	case "gratis":
		return 1
	case "pemula":
		return 3
	case "bisnis":
		return 10
	case "enterprise":
		return 999 // Unlimited
	default:
		return 1
	}
}
