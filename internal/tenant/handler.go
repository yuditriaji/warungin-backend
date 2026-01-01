package tenant

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

// GetSettings returns the tenant's settings
func (h *Handler) GetSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var tenant database.Tenant
	if err := h.db.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tenant not found"})
		return
	}

	// Parse settings JSON
	var settings database.TenantSettings
	if tenant.Settings != "" && tenant.Settings != "{}" {
		json.Unmarshal([]byte(tenant.Settings), &settings)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": settings,
	})
}

type UpdateSettingsRequest struct {
	QRISEnabled  *bool   `json:"qris_enabled"`
	QRISImageURL *string `json:"qris_image_url"`
	QRISLabel    *string `json:"qris_label"`
}

// UpdateSettings updates the tenant's settings
func (h *Handler) UpdateSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var tenant database.Tenant
	if err := h.db.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tenant not found"})
		return
	}

	// Parse existing settings
	var settings database.TenantSettings
	if tenant.Settings != "" && tenant.Settings != "{}" {
		json.Unmarshal([]byte(tenant.Settings), &settings)
	}

	// Update fields if provided
	if req.QRISEnabled != nil {
		settings.QRISEnabled = *req.QRISEnabled
	}
	if req.QRISImageURL != nil {
		settings.QRISImageURL = *req.QRISImageURL
	}
	if req.QRISLabel != nil {
		settings.QRISLabel = *req.QRISLabel
	}

	// Save settings back to JSON
	settingsJSON, _ := json.Marshal(settings)
	tenant.Settings = string(settingsJSON)

	if err := h.db.Save(&tenant).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":    settings,
		"message": "Settings updated successfully",
	})
}
