package activitylog

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

// Logger handles activity logging for audit trail
type Logger struct {
	db *gorm.DB
}

// NewLogger creates a new activity logger
func NewLogger(db *gorm.DB) *Logger {
	return &Logger{db: db}
}

// LogActivity creates an activity log entry
func (l *Logger) LogActivity(c *gin.Context, action, entityType string, entityID *uuid.UUID, details interface{}) error {
	tenantIDStr := c.GetString("tenant_id")
	tenantID, _ := uuid.Parse(tenantIDStr)
	userIDStr := c.GetString("user_id")
	userID, _ := uuid.Parse(userIDStr)

	// Get user's outlet
	var user database.User
	l.db.Where("id = ?", userID).First(&user)

	detailsJSON := ""
	if details != nil {
		if jsonBytes, err := json.Marshal(details); err == nil {
			detailsJSON = string(jsonBytes)
		}
	}

	log := database.ActivityLog{
		TenantID:   tenantID,
		UserID:     userID,
		OutletID:   user.OutletID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Details:    detailsJSON,
		IPAddress:  c.ClientIP(),
	}

	return l.db.Create(&log).Error
}

// LogCreate logs a create action
func (l *Logger) LogCreate(c *gin.Context, entityType string, entityID uuid.UUID, newData interface{}) error {
	return l.LogActivity(c, "create", entityType, &entityID, map[string]interface{}{
		"new": newData,
	})
}

// LogUpdate logs an update action with old and new values
func (l *Logger) LogUpdate(c *gin.Context, entityType string, entityID uuid.UUID, oldData, newData interface{}) error {
	return l.LogActivity(c, "update", entityType, &entityID, map[string]interface{}{
		"old": oldData,
		"new": newData,
	})
}

// LogDelete logs a delete action
func (l *Logger) LogDelete(c *gin.Context, entityType string, entityID uuid.UUID, oldData interface{}) error {
	return l.LogActivity(c, "delete", entityType, &entityID, map[string]interface{}{
		"deleted": oldData,
	})
}

// LogToggle logs a toggle active/inactive action
func (l *Logger) LogToggle(c *gin.Context, entityType string, entityID uuid.UUID, isActive bool, name string) error {
	status := "deactivated"
	if isActive {
		status = "activated"
	}
	return l.LogActivity(c, "toggle", entityType, &entityID, map[string]interface{}{
		"name":      name,
		"is_active": isActive,
		"status":    status,
	})
}
