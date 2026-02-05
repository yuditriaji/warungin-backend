package portal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"github.com/yuditriaji/warungin-backend/pkg/email"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Handler struct {
	db           *gorm.DB
	emailService *email.EmailService
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{
		db:           db,
		emailService: email.NewEmailService(),
	}
}

// ============== AUTH ==============

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	AccessToken string              `json:"access_token"`
	ExpiresIn   int64               `json:"expires_in"`
	User        database.PortalUser `json:"user"`
}

// Login authenticates portal users
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user database.PortalUser
	if err := h.db.Where("email = ? AND is_active = true", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	token, expiresIn := generatePortalToken(user)

	c.JSON(http.StatusOK, gin.H{
		"data": AuthResponse{
			AccessToken: token,
			ExpiresIn:   expiresIn,
			User:        user,
		},
	})
}

// ValidateInvite validates an invitation token
func (h *Handler) ValidateInvite(c *gin.Context) {
	token := c.Param("token")

	var invite database.PortalInvite
	if err := h.db.Where("token = ? AND status = 'pending' AND expires_at > ?", token, time.Now()).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired invitation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"email": invite.Email,
			"name":  invite.Name,
		},
	})
}

type AcceptInviteRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
	Phone    string `json:"phone"`
}

// AcceptInvite accepts invitation and creates affiliator account
func (h *Handler) AcceptInvite(c *gin.Context) {
	var req AcceptInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var invite database.PortalInvite
	if err := h.db.Where("token = ? AND status = 'pending' AND expires_at > ?", req.Token, time.Now()).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired invitation"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process password"})
		return
	}

	// Generate unique referral code
	referralCode := generateReferralCode(invite.Name)

	// Create portal user
	user := database.PortalUser{
		Email:        invite.Email,
		PasswordHash: string(hashedPassword),
		Name:         invite.Name,
		Phone:        req.Phone,
		Role:         "affiliator",
		ReferralCode: referralCode,
		IsActive:     true,
		InvitedBy:    &invite.InvitedBy,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	// Mark invite as accepted
	invite.Status = "accepted"
	h.db.Save(&invite)

	token, expiresIn := generatePortalToken(user)

	c.JSON(http.StatusOK, gin.H{
		"data": AuthResponse{
			AccessToken: token,
			ExpiresIn:   expiresIn,
			User:        user,
		},
		"message": "Account created successfully",
	})
}

// GetMe returns current user info
func (h *Handler) GetMe(c *gin.Context) {
	userID := c.GetString("portal_user_id")

	var user database.PortalUser
	if err := h.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": user})
}

// SetupSuperAdmin creates or resets the super admin account (one-time setup)
type SetupRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
	Secret   string `json:"secret" binding:"required"` // Requires PORTAL_SETUP_SECRET env var
}

func (h *Handler) SetupSuperAdmin(c *gin.Context) {
	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate setup secret
	setupSecret := os.Getenv("PORTAL_SETUP_SECRET")
	if setupSecret == "" {
		setupSecret = "warungin-setup-2024" // Default for initial setup
	}
	if req.Secret != setupSecret {
		c.JSON(http.StatusForbidden, gin.H{"error": "Invalid setup secret"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Check if user exists
	var existing database.PortalUser
	if err := h.db.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		// Update existing user
		existing.PasswordHash = string(hashedPassword)
		existing.Name = req.Name
		existing.Role = "super_admin"
		existing.IsActive = true
		h.db.Save(&existing)
		c.JSON(http.StatusOK, gin.H{"message": "Super admin password reset successfully", "email": req.Email})
		return
	}

	// Create new super admin
	user := database.PortalUser{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Role:         "super_admin",
		IsActive:     true,
	}
	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Super admin created successfully", "email": req.Email})
}

// ============== AFFILIATOR MANAGEMENT (Super Admin) ==============

type InviteAffiliatorRequest struct {
	Email string `json:"email" binding:"required,email"`
	Name  string `json:"name" binding:"required"`
}

// InviteAffiliator sends invitation to new affiliator
func (h *Handler) InviteAffiliator(c *gin.Context) {
	var req InviteAffiliatorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inviterID := c.GetString("portal_user_id")
	inviterUUID, _ := uuid.Parse(inviterID)

	// Check if email already exists
	var existing database.PortalUser
	if err := h.db.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// Check pending invite
	var pendingInvite database.PortalInvite
	if err := h.db.Where("email = ? AND status = 'pending'", req.Email).First(&pendingInvite).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Invitation already pending for this email"})
		return
	}

	// Generate invite token
	token := generateToken(32)

	invite := database.PortalInvite{
		Email:     req.Email,
		Name:      req.Name,
		Token:     token,
		InvitedBy: inviterUUID,
		Status:    "pending",
		ExpiresAt: time.Now().AddDate(0, 0, 7), // 7 days
	}

	if err := h.db.Create(&invite).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create invitation"})
		return
	}

	// Generate invite URL
	portalURL := os.Getenv("PORTAL_URL")
	if portalURL == "" {
		portalURL = "https://portal.warungin.com"
	}
	inviteURL := fmt.Sprintf("%s/accept-invite?token=%s", portalURL, token)

	// Send invitation email
	emailSent := false
	if h.emailService.IsConfigured() {
		if err := h.emailService.SendAffiliateInvitation(req.Email, req.Name, token, portalURL); err == nil {
			emailSent = true
		}
	}

	message := "Invitation created successfully and email sent to the affiliator."
	if !emailSent {
		message = "Invitation created. Email not configured - please share the invite URL manually."
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"invite":     invite,
			"invite_url": inviteURL,
			"email_sent": emailSent,
		},
		"message": message,
	})
}

// ListAffiliators returns all affiliators
func (h *Handler) ListAffiliators(c *gin.Context) {
	var affiliators []database.PortalUser
	h.db.Where("role = 'affiliator'").Order("created_at DESC").Find(&affiliators)

	// Get tenant count for each affiliator
	type AffiliatorWithStats struct {
		database.PortalUser
		TenantCount int64 `json:"tenant_count"`
	}

	result := make([]AffiliatorWithStats, len(affiliators))
	for i, aff := range affiliators {
		var count int64
		h.db.Model(&database.AffiliateTenant{}).Where("portal_user_id = ?", aff.ID).Count(&count)
		result[i] = AffiliatorWithStats{
			PortalUser:  aff,
			TenantCount: count,
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetAffiliator returns a single affiliator
func (h *Handler) GetAffiliator(c *gin.Context) {
	id := c.Param("id")

	var affiliator database.PortalUser
	if err := h.db.Where("id = ? AND role = 'affiliator'", id).First(&affiliator).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Affiliator not found"})
		return
	}

	// Get stats
	var tenantCount int64
	h.db.Model(&database.AffiliateTenant{}).Where("portal_user_id = ?", id).Count(&tenantCount)

	var totalCommission float64
	h.db.Model(&database.AffiliateEarning{}).Where("portal_user_id = ?", id).Select("COALESCE(SUM(commission_amount), 0)").Scan(&totalCommission)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"affiliator":       affiliator,
			"tenant_count":     tenantCount,
			"total_commission": totalCommission,
		},
	})
}

type UpdateAffiliatorRequest struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	BankName    string `json:"bank_name"`
	BankAccount string `json:"bank_account"`
	BankHolder  string `json:"bank_holder"`
	IsActive    *bool  `json:"is_active"`
}

// UpdateAffiliator updates affiliator details
func (h *Handler) UpdateAffiliator(c *gin.Context) {
	id := c.Param("id")

	var req UpdateAffiliatorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var affiliator database.PortalUser
	if err := h.db.Where("id = ? AND role = 'affiliator'", id).First(&affiliator).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Affiliator not found"})
		return
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Phone != "" {
		updates["phone"] = req.Phone
	}
	if req.BankName != "" {
		updates["bank_name"] = req.BankName
	}
	if req.BankAccount != "" {
		updates["bank_account"] = req.BankAccount
	}
	if req.BankHolder != "" {
		updates["bank_holder"] = req.BankHolder
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	h.db.Model(&affiliator).Updates(updates)

	c.JSON(http.StatusOK, gin.H{"data": affiliator, "message": "Affiliator updated"})
}

// DeleteAffiliator deactivates an affiliator
func (h *Handler) DeleteAffiliator(c *gin.Context) {
	id := c.Param("id")

	var affiliator database.PortalUser
	if err := h.db.Where("id = ? AND role = 'affiliator'", id).First(&affiliator).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Affiliator not found"})
		return
	}

	affiliator.IsActive = false
	h.db.Save(&affiliator)

	c.JSON(http.StatusOK, gin.H{"message": "Affiliator deactivated"})
}

// ============== TENANT MANAGEMENT ==============

// ListTenants returns all tenants with affiliate info
func (h *Handler) ListTenants(c *gin.Context) {
	type TenantWithAffiliate struct {
		database.Tenant
		AffiliatorName string `json:"affiliator_name"`
		AffiliatorID   string `json:"affiliator_id"`
	}

	var tenants []database.Tenant
	h.db.Order("created_at DESC").Find(&tenants)

	result := make([]TenantWithAffiliate, len(tenants))
	for i, t := range tenants {
		result[i] = TenantWithAffiliate{Tenant: t}

		var affTenant database.AffiliateTenant
		if err := h.db.Preload("PortalUser").Where("tenant_id = ?", t.ID).First(&affTenant).Error; err == nil {
			result[i].AffiliatorName = affTenant.PortalUser.Name
			result[i].AffiliatorID = affTenant.PortalUserID.String()
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

type AssignAffiliateRequest struct {
	PortalUserID string `json:"portal_user_id" binding:"required"`
}

// AssignAffiliate assigns a tenant to an affiliator
func (h *Handler) AssignAffiliate(c *gin.Context) {
	tenantID := c.Param("id")

	var req AssignAffiliateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tenant ID"})
		return
	}

	portalUserUUID, err := uuid.Parse(req.PortalUserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portal user ID"})
		return
	}

	// Verify tenant exists
	var tenant database.Tenant
	if err := h.db.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tenant not found"})
		return
	}

	// Verify affiliator exists
	var affiliator database.PortalUser
	if err := h.db.Where("id = ? AND role = 'affiliator' AND is_active = true", req.PortalUserID).First(&affiliator).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Affiliator not found"})
		return
	}

	// Check if already assigned
	var existing database.AffiliateTenant
	if err := h.db.Where("tenant_id = ?", tenantID).First(&existing).Error; err == nil {
		// Update existing
		existing.PortalUserID = portalUserUUID
		h.db.Save(&existing)
		c.JSON(http.StatusOK, gin.H{"message": "Affiliate assignment updated"})
		return
	}

	// Create new assignment
	affTenant := database.AffiliateTenant{
		PortalUserID: portalUserUUID,
		TenantID:     tenantUUID,
	}
	h.db.Create(&affTenant)

	c.JSON(http.StatusOK, gin.H{"message": "Tenant assigned to affiliator"})
}

// ============== EARNINGS ==============

// ListEarnings returns earnings (all for super_admin, own for affiliator)
func (h *Handler) ListEarnings(c *gin.Context) {
	role := c.GetString("portal_role")
	userID := c.GetString("portal_user_id")

	var earnings []database.AffiliateEarning
	query := h.db.Preload("PortalUser").Preload("Tenant").Order("created_at DESC")

	if role == "affiliator" {
		query = query.Where("portal_user_id = ?", userID)
	}

	query.Find(&earnings)

	// Calculate totals
	var totalPending, totalPaid float64
	for _, e := range earnings {
		if e.Status == "pending" {
			totalPending += e.CommissionAmount
		} else {
			totalPaid += e.CommissionAmount
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"earnings":      earnings,
			"total_pending": totalPending,
			"total_paid":    totalPaid,
		},
	})
}

type RecordPayoutRequest struct {
	PortalUserID string  `json:"portal_user_id" binding:"required"`
	Amount       float64 `json:"amount" binding:"required"`
	Notes        string  `json:"notes"`
}

// RecordPayout records a manual payout to affiliator
func (h *Handler) RecordPayout(c *gin.Context) {
	var req RecordPayoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	portalUserUUID, err := uuid.Parse(req.PortalUserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portal user ID"})
		return
	}

	var affiliator database.PortalUser
	if err := h.db.Where("id = ?", req.PortalUserID).First(&affiliator).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Affiliator not found"})
		return
	}

	if req.Amount > affiliator.PendingPayout {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount exceeds pending payout"})
		return
	}

	// Mark pending earnings as paid up to this amount
	now := time.Now()
	remainingAmount := req.Amount

	var pendingEarnings []database.AffiliateEarning
	h.db.Where("portal_user_id = ? AND status = 'pending'", portalUserUUID).Order("created_at ASC").Find(&pendingEarnings)

	for _, earning := range pendingEarnings {
		if remainingAmount <= 0 {
			break
		}
		if earning.CommissionAmount <= remainingAmount {
			earning.Status = "paid"
			earning.PaidAt = &now
			h.db.Save(&earning)
			remainingAmount -= earning.CommissionAmount
		}
	}

	// Update affiliator balances
	affiliator.PendingPayout -= req.Amount
	affiliator.TotalEarnings += req.Amount
	h.db.Save(&affiliator)

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Payout of Rp %.0f recorded for %s", req.Amount, affiliator.Name),
		"data": gin.H{
			"new_pending_payout": affiliator.PendingPayout,
			"total_earnings":     affiliator.TotalEarnings,
		},
	})
}

// ============== AFFILIATOR OWN DATA ==============

// MyTenants returns affiliator's referred tenants
func (h *Handler) MyTenants(c *gin.Context) {
	userID := c.GetString("portal_user_id")

	var affTenants []database.AffiliateTenant
	h.db.Preload("Tenant").Preload("Tenant.Subscription").Where("portal_user_id = ?", userID).Find(&affTenants)

	c.JSON(http.StatusOK, gin.H{"data": affTenants})
}

// MyStats returns affiliator's dashboard stats
func (h *Handler) MyStats(c *gin.Context) {
	userID := c.GetString("portal_user_id")

	var user database.PortalUser
	h.db.Where("id = ?", userID).First(&user)

	var tenantCount int64
	h.db.Model(&database.AffiliateTenant{}).Where("portal_user_id = ?", userID).Count(&tenantCount)

	var pendingEarnings, paidEarnings float64
	h.db.Model(&database.AffiliateEarning{}).Where("portal_user_id = ? AND status = 'pending'", userID).Select("COALESCE(SUM(commission_amount), 0)").Scan(&pendingEarnings)
	h.db.Model(&database.AffiliateEarning{}).Where("portal_user_id = ? AND status = 'paid'", userID).Select("COALESCE(SUM(commission_amount), 0)").Scan(&paidEarnings)

	// This month earnings
	thisMonth := time.Now().Format("2006-01")
	var thisMonthEarnings float64
	h.db.Model(&database.AffiliateEarning{}).Where("portal_user_id = ? AND created_at >= ?", userID, thisMonth+"-01").Select("COALESCE(SUM(commission_amount), 0)").Scan(&thisMonthEarnings)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"referral_code":       user.ReferralCode,
			"tenant_count":        tenantCount,
			"pending_payout":      pendingEarnings,
			"total_earned":        paidEarnings,
			"this_month_earnings": thisMonthEarnings,
		},
	})
}

// ============== REFERRAL VALIDATION (Public) ==============

// ValidateReferralCode validates a referral code
func (h *Handler) ValidateReferralCode(c *gin.Context) {
	code := c.Param("code")

	var user database.PortalUser
	if err := h.db.Where("referral_code = ? AND is_active = true AND role = 'affiliator'", code).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"valid": false, "error": "Invalid referral code"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid": true,
		"data": gin.H{
			"name": user.Name,
		},
	})
}

// ============== DASHBOARD STATS (Super Admin) ==============

// DashboardStats returns overall stats for super admin
func (h *Handler) DashboardStats(c *gin.Context) {
	var affiliatorCount, tenantCount, referredTenants int64
	var totalCommission, pendingCommission float64

	h.db.Model(&database.PortalUser{}).Where("role = 'affiliator'").Count(&affiliatorCount)
	h.db.Model(&database.Tenant{}).Count(&tenantCount)
	h.db.Model(&database.AffiliateTenant{}).Count(&referredTenants)

	h.db.Model(&database.AffiliateEarning{}).Select("COALESCE(SUM(commission_amount), 0)").Scan(&totalCommission)
	h.db.Model(&database.AffiliateEarning{}).Where("status = 'pending'").Select("COALESCE(SUM(commission_amount), 0)").Scan(&pendingCommission)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"affiliator_count":   affiliatorCount,
			"tenant_count":       tenantCount,
			"referred_tenants":   referredTenants,
			"total_commission":   totalCommission,
			"pending_commission": pendingCommission,
		},
	})
}

// ============== HELPERS ==============

func generatePortalToken(user database.PortalUser) (string, int64) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-change-in-production"
	}

	expiresIn := int64(86400 * 7) // 7 days
	claims := jwt.MapClaims{
		"portal_user_id": user.ID.String(),
		"email":          user.Email,
		"role":           user.Role,
		"exp":            time.Now().Unix() + expiresIn,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(jwtSecret))

	return tokenString, expiresIn
}

func generateToken(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func generateReferralCode(name string) string {
	// Take first word, uppercase, add random suffix
	parts := strings.Fields(strings.ToUpper(name))
	prefix := "AFF"
	if len(parts) > 0 && len(parts[0]) >= 3 {
		prefix = parts[0][:4]
		if len(prefix) > 6 {
			prefix = prefix[:6]
		}
	}
	suffix := generateToken(2) // 4 hex chars
	return fmt.Sprintf("%s%s", prefix, strings.ToUpper(suffix))
}
