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

// XenditConfig holds Xendit API configuration
type XenditConfig struct {
	APIKey       string
	WebhookToken string
	BaseURL      string
}

func getXenditConfig() XenditConfig {
	apiKey := os.Getenv("XENDIT_API_KEY")
	baseURL := os.Getenv("XENDIT_BASE_URL")
	webhookToken := os.Getenv("XENDIT_WEBHOOK_TOKEN")
	if baseURL == "" {
		baseURL = "https://api.xendit.co" // Same URL for sandbox and production
	}
	return XenditConfig{
		APIKey:       apiKey,
		WebhookToken: webhookToken,
		BaseURL:      baseURL,
	}
}

// CreateInvoiceRequest for subscription payment
type CreateInvoiceRequest struct {
	Plan        string `json:"plan" binding:"required"` // pemula, bisnis, enterprise
	Email       string `json:"email" binding:"required"`
	Description string `json:"description"`
}

// InvoiceResponse from Xendit
type InvoiceResponse struct {
	InvoiceID   string    `json:"invoice_id"`
	InvoiceURL  string    `json:"invoice_url"`
	ExternalID  string    `json:"external_id"`
	Amount      float64   `json:"amount"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
	Description string    `json:"description"`
}

// Plan prices
var PlanPrices = map[string]float64{
	"gratis":     0,
	"pemula":     49000,
	"bisnis":     149000,
	"enterprise": 0, // Custom pricing - contact sales
}

// CreateSubscriptionInvoice creates a Xendit invoice for subscription upgrade
func (h *Handler) CreateSubscriptionInvoice(c *gin.Context) {
	var req CreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	// Check plan price
	price, ok := PlanPrices[req.Plan]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid plan"})
		return
	}

	if price == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This plan doesn't require payment"})
		return
	}

	config := getXenditConfig()
	if config.APIKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Payment gateway not configured"})
		return
	}

	// Create external ID for tracking
	externalID := fmt.Sprintf("SUB-%s-%s-%d", tenantID[:8], req.Plan, time.Now().Unix())

	// Calculate PPN 11%
	ppnRate := 0.11
	ppnAmount := price * ppnRate
	totalAmount := price + ppnAmount

	// Build Xendit invoice request
	xenditReq := map[string]interface{}{
		"external_id":      externalID,
		"amount":           totalAmount,
		"payer_email":      req.Email,
		"description":      fmt.Sprintf("Warungin %s - Berlangganan Bulanan", getPlanDisplayName(req.Plan)),
		"currency":         "IDR",
		"invoice_duration": 86400, // 24 hours
		"success_redirect_url": os.Getenv("FRONTEND_URL") + "/settings?payment=success",
		"failure_redirect_url": os.Getenv("FRONTEND_URL") + "/settings?payment=failed",
		"reminder_time":    1, // Send reminder 1 hour before expiry
		"customer": map[string]interface{}{
			"email": req.Email,
		},
		"items": []map[string]interface{}{
			{
				"name":     fmt.Sprintf("Warungin %s", getPlanDisplayName(req.Plan)),
				"quantity": 1,
				"price":    price,
				"category": "Subscription",
			},
		},
		"fees": []map[string]interface{}{
			{
				"type":  "PPN 11%",
				"value": ppnAmount,
			},
		},
		"metadata": map[string]interface{}{
			"tenant_id": tenantID,
			"plan":      req.Plan,
		},
	}

	reqBody, _ := json.Marshal(xenditReq)

	// Call Xendit API
	httpReq, _ := http.NewRequest("POST", config.BaseURL+"/v2/invoices", bytes.NewBuffer(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(config.APIKey+":")))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to payment gateway"})
		return
	}
	defer resp.Body.Close()

	var xenditResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&xenditResp)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invoice creation failed", "details": xenditResp})
		return
	}

	// Extract invoice data
	invoiceID, _ := xenditResp["id"].(string)
	invoiceURL, _ := xenditResp["invoice_url"].(string)
	status, _ := xenditResp["status"].(string)
	expiryDateStr, _ := xenditResp["expiry_date"].(string)
	
	expiresAt, _ := time.Parse(time.RFC3339, expiryDateStr)

	// Store invoice in database
	tenantUUID, _ := uuid.Parse(tenantID)
	var subscription database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&subscription)

	// Generate unique invoice number using external ID
	invoiceNumber := fmt.Sprintf("INV-%s", externalID)

	invoice := database.Invoice{
		SubscriptionID: subscription.ID,
		InvoiceNumber:  invoiceNumber,
		Amount:         totalAmount, // Total including PPN
		Status:         "pending",
		DueDate:        expiresAt,
		PaymentRef:     invoiceID,
	}
	h.db.Create(&invoice)

	c.JSON(http.StatusOK, gin.H{
		"data": InvoiceResponse{
			InvoiceID:   invoiceID,
			InvoiceURL:  invoiceURL,
			ExternalID:  externalID,
			Amount:      totalAmount, // Total including PPN
			Status:      status,
			ExpiresAt:   expiresAt,
			Description: fmt.Sprintf("Warungin %s - Bulanan", getPlanDisplayName(req.Plan)),
		},
	})

	_ = tenantUUID // Silence unused variable
}

// GetInvoiceStatus checks invoice status from Xendit
func (h *Handler) GetInvoiceStatus(c *gin.Context) {
	invoiceID := c.Param("invoice_id")

	config := getXenditConfig()
	if config.APIKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Payment gateway not configured"})
		return
	}

	// Call Xendit API
	httpReq, _ := http.NewRequest("GET", config.BaseURL+"/v2/invoices/"+invoiceID, nil)
	httpReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(config.APIKey+":")))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check invoice status"})
		return
	}
	defer resp.Body.Close()

	var xenditResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&xenditResp)

	status, _ := xenditResp["status"].(string)
	paidAt, _ := xenditResp["paid_at"].(string)
	paymentMethod, _ := xenditResp["payment_method"].(string)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"invoice_id":     invoiceID,
			"status":         status,
			"paid_at":        paidAt,
			"payment_method": paymentMethod,
		},
	})
}

// XenditWebhook handles Xendit invoice callbacks
func (h *Handler) XenditWebhook(c *gin.Context) {
	// Verify webhook token
	config := getXenditConfig()
	callbackToken := c.GetHeader("x-callback-token")
	if config.WebhookToken != "" && callbackToken != config.WebhookToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid callback token"})
		return
	}

	var notification map[string]interface{}
	if err := c.ShouldBindJSON(&notification); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Log the webhook for debugging
	fmt.Printf("Xendit Webhook: %+v\n", notification)

	invoiceID, _ := notification["id"].(string)
	externalID, _ := notification["external_id"].(string)
	status, _ := notification["status"].(string)

	// Parse tenant_id and plan from external_id (format: SUB-{tenant_id_prefix}-{plan}-{timestamp})
	// Example: SUB-b89beb65-bisnis-1769014518
	var tenantID, plan string
	
	// Try to get from metadata first (may not be available in webhook)
	if metadata, ok := notification["metadata"].(map[string]interface{}); ok {
		tenantID, _ = metadata["tenant_id"].(string)
		plan, _ = metadata["plan"].(string)
	}
	
	// If metadata not available, parse from external_id
	if tenantID == "" || plan == "" {
		// External ID format: SUB-{tenant_id_first_8_chars}-{plan}-{timestamp}
		// We need to look up the full tenant_id from the invoice -> subscription
		var inv database.Invoice
		if err := h.db.Where("payment_ref = ?", invoiceID).First(&inv).Error; err == nil {
			// Get tenant_id from subscription
			var sub database.Subscription
			if err := h.db.Where("id = ?", inv.SubscriptionID).First(&sub).Error; err == nil {
				tenantID = sub.TenantID.String()
				fmt.Printf("Found tenant_id from subscription: %s\n", tenantID)
			} else {
				fmt.Printf("Failed to find subscription for invoice: %v\n", err)
			}
		} else {
			fmt.Printf("Failed to find invoice for payment_ref: %v\n", err)
		}
		
		// Parse plan from external_id
		parts := splitExternalID(externalID)
		if len(parts) >= 3 {
			plan = parts[2] // SUB-{prefix}-{plan}-{timestamp}
		}
	}
	
	fmt.Printf("Webhook Processing: status=%s, tenantID=%s, plan=%s\n", status, tenantID, plan)

	// Find invoice
	var invoice database.Invoice
	if err := h.db.Where("payment_ref = ?", invoiceID).First(&invoice).Error; err != nil {
		// Invoice not found, log but don't fail
		fmt.Printf("Invoice not found for webhook: %s\n", invoiceID)
	}

	// Handle based on status
	switch status {
	case "PAID", "SETTLED":
		// Update invoice
		invoice.Status = "paid"
		invoice.PaidAt = timePtr(time.Now())
		h.db.Save(&invoice)

		// Upgrade subscription
		if tenantID != "" && plan != "" {
			if err := h.upgradeSubscription(tenantID, plan); err != nil {
				fmt.Printf("Failed to upgrade subscription: %v\n", err)
			} else {
				fmt.Printf("Successfully upgraded subscription for tenant %s to plan %s\n", tenantID, plan)
			}
		} else {
			fmt.Printf("Warning: Could not upgrade subscription - missing tenantID or plan\n")
		}
		
	case "EXPIRED":
		invoice.Status = "voided"
		h.db.Save(&invoice)
		
	case "PENDING":
		invoice.Status = "pending"
		h.db.Save(&invoice)
	}

	_ = externalID // Silence unused variable

	c.JSON(http.StatusOK, gin.H{"message": "OK"})
}

// upgradeSubscription upgrades a tenant's subscription
func (h *Handler) upgradeSubscription(tenantID string, plan string) error {
	var subscription database.Subscription
	if err := h.db.Where("tenant_id = ?", tenantID).First(&subscription).Error; err != nil {
		return err
	}

	// Plan limits
	planLimits := map[string]struct {
		MaxUsers               int
		MaxProducts            int
		MaxTransactionsDaily   int
		MaxTransactionsMonthly int
		MaxOutlets             int
		DataRetentionDays      int
	}{
		"pemula": {
			MaxUsers:               3,
			MaxProducts:            200,
			MaxTransactionsDaily:   0, // Unlimited
			MaxTransactionsMonthly: 0, // Unlimited
			MaxOutlets:             1,
			DataRetentionDays:      365,
		},
		"bisnis": {
			MaxUsers:               10,
			MaxProducts:            0, // Unlimited
			MaxTransactionsDaily:   0,
			MaxTransactionsMonthly: 0,
			MaxOutlets:             3,
			DataRetentionDays:      365 * 3,
		},
		"enterprise": {
			MaxUsers:               0, // Unlimited
			MaxProducts:            0,
			MaxTransactionsDaily:   0,
			MaxTransactionsMonthly: 0,
			MaxOutlets:             0, // Unlimited
			DataRetentionDays:      0, // Forever
		},
	}

	limits, ok := planLimits[plan]
	if !ok {
		return fmt.Errorf("invalid plan: %s", plan)
	}

	// Get the first outlet for this tenant to migrate orphaned data
	var firstOutlet database.Outlet
	if err := h.db.Where("tenant_id = ?", tenantID).Order("created_at ASC").First(&firstOutlet).Error; err == nil {
		// Migrate products with NULL outlet_id to first outlet
		h.db.Model(&database.Product{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		// Migrate materials with NULL outlet_id to first outlet
		h.db.Model(&database.RawMaterial{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		// Migrate users with NULL outlet_id to first outlet
		h.db.Model(&database.User{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		fmt.Printf("Migrated orphaned products/materials/users to outlet %s for tenant %s\n", firstOutlet.ID, tenantID)
	}

	subscription.Plan = plan
	subscription.Status = "active"
	subscription.MaxUsers = limits.MaxUsers
	subscription.MaxProducts = limits.MaxProducts
	subscription.MaxTransactionsDaily = limits.MaxTransactionsDaily
	subscription.MaxTransactionsMonthly = limits.MaxTransactionsMonthly
	subscription.MaxOutlets = limits.MaxOutlets
	subscription.DataRetentionDays = limits.DataRetentionDays
	subscription.CurrentPeriodStart = time.Now()
	subscription.CurrentPeriodEnd = time.Now().AddDate(0, 1, 0) // 1 month

	return h.db.Save(&subscription).Error
}

// QRIS payment for POS transactions (keeping this for POS functionality)
type CreateQRISRequest struct {
	TransactionID string `json:"transaction_id" binding:"required"`
}

type QRISResponse struct {
	QRString    string    `json:"qr_string"`
	QRImageURL  string    `json:"qr_image_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	OrderID     string    `json:"order_id"`
	GrossAmount float64   `json:"gross_amount"`
}

// CreateQRIS creates a QRIS payment for a POS transaction
// Note: This uses your own QRIS configuration, not Xendit
func (h *Handler) CreateQRIS(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":   "QRIS via Xendit not yet implemented",
		"message": "Please use your own QRIS merchant for POS transactions. Xendit is used for subscription payments only.",
	})
}

// CheckStatus checks payment status (legacy endpoint for compatibility)
func (h *Handler) CheckStatus(c *gin.Context) {
	orderID := c.Param("order_id")
	
	// Check if it's an invoice
	if len(orderID) > 3 && orderID[:3] == "SUB" {
		// It's a subscription invoice - redirect to invoice status
		c.JSON(http.StatusOK, gin.H{
			"message": "Use /api/v1/payment/invoice/{invoice_id}/status for subscription payments",
		})
		return
	}

	// For POS transactions
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"order_id": orderID,
			"status":   "please check via your merchant app",
		},
	})
}

// Webhook handles legacy webhook (redirects to XenditWebhook)
func (h *Handler) Webhook(c *gin.Context) {
	// Check if it's a Xendit callback
	if c.GetHeader("x-callback-token") != "" {
		h.XenditWebhook(c)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "OK"})
}

// WebhookVerify handles GET requests for webhook URL verification
func (h *Handler) WebhookVerify(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "OK",
		"message": "Webhook endpoint is active",
		"service": "warungin",
	})
}

// Helper functions
func getPlanDisplayName(plan string) string {
	names := map[string]string{
		"gratis":     "Gratis",
		"pemula":     "Pemula",
		"bisnis":     "Bisnis",
		"enterprise": "Enterprise",
	}
	if name, ok := names[plan]; ok {
		return name
	}
	return plan
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// Helper to generate unique transaction ID
func generateTransactionID() string {
	return uuid.New().String()
}

// splitExternalID splits external_id into parts
// Format: SUB-{tenant_prefix}-{plan}-{timestamp}
func splitExternalID(externalID string) []string {
	// Split by "-" but handle the case where there might be dashes in tenant ID
	// Example: SUB-b89beb65-bisnis-1769014518
	parts := make([]string, 0)
	if len(externalID) < 4 || externalID[:4] != "SUB-" {
		return parts
	}
	
	remaining := externalID[4:] // Remove "SUB-"
	parts = append(parts, "SUB")
	
	// Find the plan part (known values: gratis, pemula, bisnis, enterprise)
	knownPlans := []string{"enterprise", "bisnis", "pemula", "gratis"}
	for _, plan := range knownPlans {
		if idx := findPlanIndex(remaining, plan); idx != -1 {
			parts = append(parts, remaining[:idx-1]) // tenant prefix
			parts = append(parts, plan)
			if idx+len(plan) < len(remaining) {
				parts = append(parts, remaining[idx+len(plan)+1:]) // timestamp
			}
			return parts
		}
	}
	
	return parts
}

// findPlanIndex finds the index of plan in the string
func findPlanIndex(s, plan string) int {
	for i := 0; i <= len(s)-len(plan); i++ {
		if s[i:i+len(plan)] == plan {
			// Check if it's bounded by dashes
			beforeOK := i == 0 || s[i-1] == '-'
			afterOK := i+len(plan) == len(s) || s[i+len(plan)] == '-'
			if beforeOK && afterOK {
				return i
			}
		}
	}
	return -1
}

