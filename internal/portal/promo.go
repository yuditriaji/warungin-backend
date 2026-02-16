package portal

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
)

// ============== PROMO CODE MANAGEMENT (Super Admin) ==============

// CreatePromoCodeRequest represents the request to create a promo code
type CreatePromoCodeRequest struct {
	Code            string  `json:"code" binding:"required"`            // 6 alphanumeric chars
	ReferralCode    *string `json:"referral_code"`                      // Optional: 6 chars matching an active affiliator
	DiscountType    string  `json:"discount_type" binding:"required"`   // "percentage" or "fixed"
	DiscountValue   float64 `json:"discount_value" binding:"required"`  // Value of discount
	ValidFrom       string  `json:"valid_from" binding:"required"`      // ISO 8601 date string
	ValidUntil      string  `json:"valid_until" binding:"required"`     // ISO 8601 date string
	MaxUses         *int    `json:"max_uses"`                           // null = unlimited
	ApplicablePlans string  `json:"applicable_plans"`                   // comma-separated: "pemula,bisnis" or "" for all
}

// UpdatePromoCodeRequest represents the request to update a promo code
type UpdatePromoCodeRequest struct {
	ReferralCode    *string  `json:"referral_code"`    // Can only be set if currently null
	DiscountType    *string  `json:"discount_type"`
	DiscountValue   *float64 `json:"discount_value"`
	ValidFrom       *string  `json:"valid_from"`
	ValidUntil      *string  `json:"valid_until"`
	MaxUses         *int     `json:"max_uses"`
	ApplicablePlans *string  `json:"applicable_plans"`
	IsActive        *bool    `json:"is_active"`
}

// CreatePromoCode creates a new promo code
func (h *Handler) CreatePromoCode(c *gin.Context) {
	var req CreatePromoCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user ID from context
	userIDStr := c.GetString("portal_user_id")
	createdBy, err := uuid.Parse(userIDStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user"})
		return
	}

	// Validate and normalize code (auto-uppercase, 3-10 alphanumeric chars)
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if len(code) < 3 || len(code) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kode promo harus 3-10 karakter"})
		return
	}
	for _, ch := range code {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Kode promo harus alfanumerik (huruf dan angka)"})
			return
		}
	}

	// Check code uniqueness
	var existingCode database.PromoCode
	if err := h.db.Where("code = ?", code).First(&existingCode).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Kode promo sudah digunakan"})
		return
	}

	// Validate discount type
	if req.DiscountType != "percentage" && req.DiscountType != "fixed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tipe diskon harus 'percentage' atau 'fixed'"})
		return
	}

	// Validate discount value
	if req.DiscountType == "percentage" {
		if req.DiscountValue < 1 || req.DiscountValue > 99 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Diskon persentase harus antara 1-99%"})
			return
		}
	} else {
		if req.DiscountValue <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Nilai diskon harus lebih besar dari 0"})
			return
		}
	}

	// Parse dates
	validFrom, err := time.Parse("2006-01-02", req.ValidFrom)
	if err != nil {
		validFrom, err = time.Parse(time.RFC3339, req.ValidFrom)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Format tanggal valid_from tidak valid (gunakan YYYY-MM-DD)"})
			return
		}
	}
	validUntil, err := time.Parse("2006-01-02", req.ValidUntil)
	if err != nil {
		validUntil, err = time.Parse(time.RFC3339, req.ValidUntil)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Format tanggal valid_until tidak valid (gunakan YYYY-MM-DD)"})
			return
		}
	}
	// Set valid_until to end of day
	validUntil = validUntil.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	if validUntil.Before(validFrom) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tanggal selesai harus setelah tanggal mulai"})
		return
	}

	// Handle referral code
	var referralCode *string
	fullCode := code

	if req.ReferralCode != nil && *req.ReferralCode != "" {
		refCode := strings.ToUpper(strings.TrimSpace(*req.ReferralCode))

		// Validate referral code exists as an active affiliator
		var affiliator database.PortalUser
		if err := h.db.Where("referral_code = ? AND is_active = true AND role = 'affiliator'", refCode).First(&affiliator).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Kode referral '%s' tidak ditemukan atau tidak aktif", refCode)})
			return
		}

		referralCode = &refCode
		fullCode = refCode + code
	}

	// Check full code uniqueness
	if err := h.db.Where("full_code = ?", fullCode).First(&existingCode).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Kode promo lengkap sudah digunakan"})
		return
	}

	// Create promo code
	promo := database.PromoCode{
		Code:            code,
		ReferralCode:    referralCode,
		FullCode:        fullCode,
		DiscountType:    req.DiscountType,
		DiscountValue:   req.DiscountValue,
		ValidFrom:       validFrom,
		ValidUntil:      validUntil,
		MaxUses:         req.MaxUses,
		ApplicablePlans: req.ApplicablePlans,
		IsActive:        true,
		CreatedBy:       createdBy,
	}

	if err := h.db.Create(&promo).Error; err != nil {
		fmt.Printf("Create promo code error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat kode promo"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    "Kode promo berhasil dibuat",
		"promo_code": promo,
	})
}

// ListPromoCodes returns all promo codes with pagination
func (h *Handler) ListPromoCodes(c *gin.Context) {
	var promoCodes []database.PromoCode

	query := h.db.Order("created_at DESC")

	// Filter by status
	status := c.Query("status")
	if status == "active" {
		query = query.Where("is_active = true AND valid_until >= ?", time.Now())
	} else if status == "inactive" {
		query = query.Where("is_active = false OR valid_until < ?", time.Now())
	}

	if err := query.Find(&promoCodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data promo"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"promo_codes": promoCodes,
		"total":       len(promoCodes),
	})
}

// GetPromoCode returns a single promo code with details
func (h *Handler) GetPromoCode(c *gin.Context) {
	id := c.Param("id")

	var promo database.PromoCode
	if err := h.db.Where("id = ?", id).First(&promo).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Kode promo tidak ditemukan"})
		return
	}

	// Get usage count
	var usageCount int64
	h.db.Model(&database.PromoCodeUsage{}).Where("promo_code_id = ?", promo.ID).Count(&usageCount)

	c.JSON(http.StatusOK, gin.H{
		"promo_code":  promo,
		"usage_count": usageCount,
	})
}

// UpdatePromoCode updates an existing promo code
func (h *Handler) UpdatePromoCode(c *gin.Context) {
	id := c.Param("id")

	var promo database.PromoCode
	if err := h.db.Where("id = ?", id).First(&promo).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Kode promo tidak ditemukan"})
		return
	}

	var req UpdatePromoCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Handle referral code update (immutable once set)
	if req.ReferralCode != nil && *req.ReferralCode != "" {
		if promo.ReferralCode != nil && *promo.ReferralCode != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Kode referral tidak dapat diubah setelah ditetapkan"})
			return
		}

		refCode := strings.ToUpper(strings.TrimSpace(*req.ReferralCode))

		// Validate referral code exists
		var affiliator database.PortalUser
		if err := h.db.Where("referral_code = ? AND is_active = true AND role = 'affiliator'", refCode).First(&affiliator).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Kode referral '%s' tidak ditemukan atau tidak aktif", refCode)})
			return
		}

		// Update full code
		newFullCode := refCode + promo.Code
		var existing database.PromoCode
		if err := h.db.Where("full_code = ? AND id != ?", newFullCode, promo.ID).First(&existing).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Kode promo lengkap sudah digunakan"})
			return
		}

		promo.ReferralCode = &refCode
		promo.FullCode = newFullCode
	}

	if req.DiscountType != nil {
		if *req.DiscountType != "percentage" && *req.DiscountType != "fixed" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tipe diskon harus 'percentage' atau 'fixed'"})
			return
		}
		promo.DiscountType = *req.DiscountType
	}

	if req.DiscountValue != nil {
		if promo.DiscountType == "percentage" {
			if *req.DiscountValue < 1 || *req.DiscountValue > 99 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Diskon persentase harus antara 1-99%"})
				return
			}
		} else if *req.DiscountValue <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Nilai diskon harus lebih besar dari 0"})
			return
		}
		promo.DiscountValue = *req.DiscountValue
	}

	if req.ValidFrom != nil {
		validFrom, err := time.Parse("2006-01-02", *req.ValidFrom)
		if err != nil {
			validFrom, err = time.Parse(time.RFC3339, *req.ValidFrom)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Format tanggal valid_from tidak valid"})
				return
			}
		}
		promo.ValidFrom = validFrom
	}

	if req.ValidUntil != nil {
		validUntil, err := time.Parse("2006-01-02", *req.ValidUntil)
		if err != nil {
			validUntil, err = time.Parse(time.RFC3339, *req.ValidUntil)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Format tanggal valid_until tidak valid"})
				return
			}
		}
		validUntil = validUntil.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		promo.ValidUntil = validUntil
	}

	if promo.ValidUntil.Before(promo.ValidFrom) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tanggal selesai harus setelah tanggal mulai"})
		return
	}

	if req.MaxUses != nil {
		promo.MaxUses = req.MaxUses
	}

	if req.ApplicablePlans != nil {
		promo.ApplicablePlans = *req.ApplicablePlans
	}

	if req.IsActive != nil {
		promo.IsActive = *req.IsActive
	}

	if err := h.db.Save(&promo).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui kode promo"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Kode promo berhasil diperbarui",
		"promo_code": promo,
	})
}

// DeactivatePromoCode soft-deactivates a promo code
func (h *Handler) DeactivatePromoCode(c *gin.Context) {
	id := c.Param("id")

	var promo database.PromoCode
	if err := h.db.Where("id = ?", id).First(&promo).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Kode promo tidak ditemukan"})
		return
	}

	promo.IsActive = false
	h.db.Save(&promo)

	c.JSON(http.StatusOK, gin.H{"message": "Kode promo berhasil dinonaktifkan"})
}

// GetPromoCodeUsages returns usage history for a promo code
func (h *Handler) GetPromoCodeUsages(c *gin.Context) {
	id := c.Param("id")

	var usages []database.PromoCodeUsage
	if err := h.db.Where("promo_code_id = ?", id).
		Preload("Tenant").
		Order("created_at DESC").
		Find(&usages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data penggunaan promo"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"usages": usages,
		"total":  len(usages),
	})
}
