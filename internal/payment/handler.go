package payment

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

type MidtransConfig struct {
	ServerKey string
	BaseURL   string
}

func getMidtransConfig() MidtransConfig {
	serverKey := os.Getenv("MIDTRANS_SERVER_KEY")
	baseURL := os.Getenv("MIDTRANS_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.sandbox.midtrans.com" // Default to sandbox
	}
	return MidtransConfig{
		ServerKey: serverKey,
		BaseURL:   baseURL,
	}
}

type CreateQRISRequest struct {
	TransactionID string `json:"transaction_id" binding:"required"`
}

type QRISResponse struct {
	QRString     string    `json:"qr_string"`
	QRImageURL   string    `json:"qr_image_url"`
	ExpiresAt    time.Time `json:"expires_at"`
	OrderID      string    `json:"order_id"`
	GrossAmount  float64   `json:"gross_amount"`
}

// CreateQRIS creates a QRIS payment for a transaction
func (h *Handler) CreateQRIS(c *gin.Context) {
	var req CreateQRISRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	// Get transaction
	var transaction database.Transaction
	if err := h.db.Where("id = ? AND tenant_id = ?", req.TransactionID, tenantID).
		First(&transaction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not found"})
		return
	}

	config := getMidtransConfig()
	if config.ServerKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Midtrans not configured"})
		return
	}

	// Create order ID
	orderID := fmt.Sprintf("WRG-%s-%d", transaction.ID.String()[:8], time.Now().Unix())

	// Build Midtrans request
	midtransReq := map[string]interface{}{
		"payment_type": "qris",
		"transaction_details": map[string]interface{}{
			"order_id":     orderID,
			"gross_amount": int(transaction.Total),
		},
		"qris": map[string]interface{}{
			"acquirer": "gopay", // Can be gopay, airpay, etc
		},
	}

	reqBody, _ := json.Marshal(midtransReq)

	// Call Midtrans API
	httpReq, _ := http.NewRequest("POST", config.BaseURL+"/v2/charge", bytes.NewBuffer(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(config.ServerKey+":")))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to payment gateway"})
		return
	}
	defer resp.Body.Close()

	var midtransResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&midtransResp)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Payment creation failed", "details": midtransResp})
		return
	}

	// Extract QR data
	actions, _ := midtransResp["actions"].([]interface{})
	var qrString, qrImageURL string
	for _, action := range actions {
		a := action.(map[string]interface{})
		if a["name"] == "generate-qr-code" {
			qrImageURL, _ = a["url"].(string)
		}
	}
	qrString, _ = midtransResp["qr_string"].(string)

	// Update transaction with payment reference
	transaction.PaymentRef = orderID
	transaction.Status = "pending"
	h.db.Save(&transaction)

	expiresAt := time.Now().Add(15 * time.Minute)

	c.JSON(http.StatusOK, gin.H{
		"data": QRISResponse{
			QRString:    qrString,
			QRImageURL:  qrImageURL,
			ExpiresAt:   expiresAt,
			OrderID:     orderID,
			GrossAmount: transaction.Total,
		},
	})
}

// CheckStatus checks payment status
func (h *Handler) CheckStatus(c *gin.Context) {
	orderID := c.Param("order_id")
	tenantID := c.GetString("tenant_id")

	config := getMidtransConfig()
	if config.ServerKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Midtrans not configured"})
		return
	}

	// Call Midtrans API
	httpReq, _ := http.NewRequest("GET", config.BaseURL+"/v2/"+orderID+"/status", nil)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(config.ServerKey+":")))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check payment status"})
		return
	}
	defer resp.Body.Close()

	var midtransResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&midtransResp)

	status, _ := midtransResp["transaction_status"].(string)
	
	// Update transaction if paid
	if status == "settlement" || status == "capture" {
		h.db.Model(&database.Transaction{}).
			Where("payment_ref = ? AND tenant_id = ?", orderID, tenantID).
			Update("status", "completed")
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"order_id":           orderID,
			"transaction_status": status,
			"payment_type":       midtransResp["payment_type"],
		},
	})
}

// Webhook handles Midtrans notifications
func (h *Handler) Webhook(c *gin.Context) {
	var notification map[string]interface{}
	if err := c.ShouldBindJSON(&notification); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	orderID, _ := notification["order_id"].(string)
	status, _ := notification["transaction_status"].(string)

	// Extract tenant from order ID or lookup
	var transaction database.Transaction
	if err := h.db.Where("payment_ref = ?", orderID).First(&transaction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not found"})
		return
	}

	// Update based on status
	switch status {
	case "settlement", "capture":
		transaction.Status = "completed"
	case "pending":
		transaction.Status = "pending"
	case "deny", "cancel", "expire":
		transaction.Status = "voided"
	}

	h.db.Save(&transaction)

	c.JSON(http.StatusOK, gin.H{"message": "OK"})
}

// Helper to generate unique transaction ID
func generateTransactionID() string {
	return uuid.New().String()
}
