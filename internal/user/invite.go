package user

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/activitylog"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"github.com/yuditriaji/warungin-backend/pkg/email"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type InviteHandler struct {
	db           *gorm.DB
	logger       *activitylog.Logger
	emailService *email.EmailService
}

func NewInviteHandler(db *gorm.DB) *InviteHandler {
	return &InviteHandler{
		db:           db,
		logger:       activitylog.NewLogger(db),
		emailService: email.NewEmailService(),
	}
}

type InviteStaffInput struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Role     string `json:"role" binding:"required,oneof=manager cashier"`
	OutletID string `json:"outlet_id"`
}

type AcceptInviteInput struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

// generateInviteToken creates a random token for invitation
func generateInviteToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// InviteStaff creates a pending staff invitation and sends email
func (h *InviteHandler) InviteStaff(c *gin.Context) {
	userRole := c.GetString("role")
	if userRole != "owner" && userRole != "manager" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owner or manager can invite staff"})
		return
	}

	var input InviteStaffInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	tenantUUID, _ := uuid.Parse(tenantID)

	// Check subscription limit
	var staffCount int64
	h.db.Model(&database.User{}).Where("tenant_id = ? AND role != 'owner'", tenantID).Count(&staffCount)

	var sub database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&sub)

	maxUsers := getMaxUsers(sub.Plan)
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

	// Check if email already exists
	var existing database.User
	if h.db.Where("email = ?", input.Email).First(&existing).Error == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email already registered"})
		return
	}

	// Check if pending invite exists
	var existingInvite database.StaffInvite
	if h.db.Where("email = ? AND tenant_id = ? AND status = 'pending'", input.Email, tenantID).First(&existingInvite).Error == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation already sent to this email"})
		return
	}

	// Create invite token
	token := generateInviteToken()
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 days

	var outletID *uuid.UUID
	if input.OutletID != "" {
		uuidVal, _ := uuid.Parse(input.OutletID)
		outletID = &uuidVal
	}

	invite := database.StaffInvite{
		TenantID:  tenantUUID,
		OutletID:  outletID,
		Email:     input.Email,
		Name:      input.Name,
		Role:      input.Role,
		Token:     token,
		Status:    "pending",
		ExpiresAt: expiresAt,
	}

	if err := h.db.Create(&invite).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create invitation"})
		return
	}

	// Get tenant name for email
	var tenant database.Tenant
	h.db.First(&tenant, "id = ?", tenantID)

	// Send invitation email
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	if h.emailService.IsConfigured() {
		err := h.emailService.SendStaffInvitation(input.Email, input.Name, tenant.Name, token, frontendURL)
		if err != nil {
			// Log error but don't fail - invitation is created
			c.JSON(http.StatusCreated, gin.H{
				"data":    invite,
				"warning": "Invitation created but email failed to send",
			})
			return
		}
	}

	// Log activity
	h.logger.LogCreate(c, "staff_invite", invite.ID, map[string]interface{}{
		"email": invite.Email,
		"name":  invite.Name,
		"role":  invite.Role,
	})

	c.JSON(http.StatusCreated, gin.H{
		"data":    invite,
		"message": "Invitation sent successfully",
	})
}

// GetPendingInvites lists all pending invitations for tenant
func (h *InviteHandler) GetPendingInvites(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var invites []database.StaffInvite
	h.db.Where("tenant_id = ? AND status = 'pending'", tenantID).
		Order("created_at DESC").
		Find(&invites)

	c.JSON(http.StatusOK, gin.H{"data": invites})
}

// CancelInvite cancels a pending invitation
func (h *InviteHandler) CancelInvite(c *gin.Context) {
	userRole := c.GetString("role")
	if userRole != "owner" && userRole != "manager" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Permission denied"})
		return
	}

	tenantID := c.GetString("tenant_id")
	inviteID := c.Param("id")

	var invite database.StaffInvite
	if err := h.db.Where("id = ? AND tenant_id = ?", inviteID, tenantID).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found"})
		return
	}

	invite.Status = "cancelled"
	h.db.Save(&invite)

	c.JSON(http.StatusOK, gin.H{"message": "Invitation cancelled"})
}

// ValidateInvite checks if an invite token is valid (public endpoint)
func (h *InviteHandler) ValidateInvite(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token required"})
		return
	}

	var invite database.StaffInvite
	if err := h.db.Preload("Tenant").Where("token = ? AND status = 'pending'", token).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired invitation"})
		return
	}

	if time.Now().After(invite.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation has expired"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": map[string]interface{}{
			"name":        invite.Name,
			"email":       invite.Email,
			"role":        invite.Role,
			"tenant_name": invite.Tenant.Name,
		},
	})
}

// AcceptInvite creates the user account from invitation (public endpoint)
func (h *InviteHandler) AcceptInvite(c *gin.Context) {
	var input AcceptInviteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var invite database.StaffInvite
	if err := h.db.Where("token = ? AND status = 'pending'", input.Token).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired invitation"})
		return
	}

	if time.Now().After(invite.ExpiresAt) {
		invite.Status = "expired"
		h.db.Save(&invite)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation has expired"})
		return
	}

	// Check if email already taken (race condition check)
	var existing database.User
	if h.db.Where("email = ?", invite.Email).First(&existing).Error == nil {
		invite.Status = "cancelled"
		h.db.Save(&invite)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email already registered"})
		return
	}

	// Create user account
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)

	user := database.User{
		TenantID:     invite.TenantID,
		OutletID:     invite.OutletID,
		Email:        invite.Email,
		Name:         invite.Name,
		Role:         invite.Role,
		PasswordHash: string(hashedPassword),
		IsActive:     true,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	// Mark invite as accepted
	invite.Status = "accepted"
	h.db.Save(&invite)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully. You can now login.",
	})
}

// ResendInvite resends the invitation email
func (h *InviteHandler) ResendInvite(c *gin.Context) {
	userRole := c.GetString("role")
	if userRole != "owner" && userRole != "manager" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Permission denied"})
		return
	}

	tenantID := c.GetString("tenant_id")
	inviteID := c.Param("id")

	var invite database.StaffInvite
	if err := h.db.Where("id = ? AND tenant_id = ? AND status = 'pending'", inviteID, tenantID).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found"})
		return
	}

	// Refresh token and expiry
	invite.Token = generateInviteToken()
	invite.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	h.db.Save(&invite)

	// Get tenant name
	var tenant database.Tenant
	h.db.First(&tenant, "id = ?", tenantID)

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	if h.emailService.IsConfigured() {
		if err := h.emailService.SendStaffInvitation(invite.Email, invite.Name, tenant.Name, invite.Token, frontendURL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send email"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation resent"})
}
