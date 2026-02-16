package payment

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"github.com/yuditriaji/warungin-backend/pkg/email"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

// BillingPeriod represents a subscription billing cycle
type BillingPeriod string

const (
	BillingMonthly   BillingPeriod = "monthly"
	BillingQuarterly BillingPeriod = "quarterly"
	BillingYearly    BillingPeriod = "yearly"
)

// PlanPricing holds prices for each billing period
type PlanPricing struct {
	Monthly   float64
	Quarterly float64
	Yearly    float64
}

// GetPrice returns the price for a specific billing period
func (p PlanPricing) GetPrice(period BillingPeriod) float64 {
	switch period {
	case BillingQuarterly:
		return p.Quarterly
	case BillingYearly:
		return p.Yearly
	default:
		return p.Monthly
	}
}

// GetPeriodMonths returns the number of months for a billing period
func GetPeriodMonths(period BillingPeriod) int {
	switch period {
	case BillingQuarterly:
		return 3
	case BillingYearly:
		return 12
	default:
		return 1
	}
}

// ValidBillingPeriod checks if a billing period string is valid
func ValidBillingPeriod(period string) bool {
	switch BillingPeriod(period) {
	case BillingMonthly, BillingQuarterly, BillingYearly:
		return true
	}
	return false
}

// PlanPrices defines pricing for each plan across billing periods
var PlanPrices = map[string]PlanPricing{
	"gratis":     {Monthly: 0, Quarterly: 0, Yearly: 0},
	"pemula":     {Monthly: 49000, Quarterly: 132000, Yearly: 470000},
	"bisnis":     {Monthly: 149000, Quarterly: 399000, Yearly: 1430000},
	"enterprise": {Monthly: 0, Quarterly: 0, Yearly: 0},
}

// --- QRIS Subscription Payment ---

// CreateQRISSubscriptionRequest is the request to generate a QRIS for subscription payment
type CreateQRISSubscriptionRequest struct {
	Plan          string `json:"plan" binding:"required"`
	BillingPeriod string `json:"billing_period" binding:"required"`
	Email         string `json:"email" binding:"required"`
}

// AdminFee is the flat administration fee per transaction (Rp 2,500)
const AdminFee = 2500.0

// QRISSubscriptionResponse is returned to the frontend with QRIS data
type QRISSubscriptionResponse struct {
	QRContent   string    `json:"qr_content"`
	QRImageURL  string    `json:"qr_image_url"`
	Amount      float64   `json:"amount"`
	BaseAmount  float64   `json:"base_amount"`
	AdminFee    float64   `json:"admin_fee"`
	ExpiresAt   time.Time `json:"expires_at"`
	ReferenceNo string    `json:"reference_no"`
	Plan        string    `json:"plan"`
	Period      string    `json:"billing_period"`
}

// CreateSubscriptionQRIS generates a Doku QRIS code for subscription payment
func (h *Handler) CreateSubscriptionQRIS(c *gin.Context) {
	var req CreateQRISSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	// Validate billing period
	if !ValidBillingPeriod(req.BillingPeriod) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Periode billing tidak valid. Pilih: monthly, quarterly, yearly"})
		return
	}

	// Check plan price
	pricing, ok := PlanPrices[req.Plan]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket tidak valid"})
		return
	}

	period := BillingPeriod(req.BillingPeriod)
	basePrice := pricing.GetPrice(period)

	if basePrice == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket ini tidak memerlukan pembayaran"})
		return
	}

	// Calculate admin fee (flat Rp 2,500)
	adminFee := AdminFee
	totalAmount := basePrice + adminFee

	// Get Doku config
	config, err := getDokuConfig()
	if err != nil {
		fmt.Printf("Doku config error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Payment gateway belum dikonfigurasi"})
		return
	}

	// Get B2B access token
	accessToken, err := getB2BAccessToken(config)
	if err != nil {
		fmt.Printf("Doku token error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghubungi payment gateway"})
		return
	}

	// Create reference number
	referenceNo := fmt.Sprintf("WSUB-%s-%s-%s-%d", tenantID[:8], req.Plan, req.BillingPeriod[:1], time.Now().Unix())

	// Call Doku Generate QRIS
	merchantID := config.ClientID
	qrisReq := DokuQRISRequest{
		PartnerReferenceNo: referenceNo,
		Amount: DokuAmount{
			Value:    fmt.Sprintf("%.2f", totalAmount),
			Currency: "IDR",
		},
		MerchantID:     merchantID,
		ValidityPeriod: "PT30M", // 30 minutes
		AdditionalInfo: &DokuAdditional{
			Description: fmt.Sprintf("Warungin %s - %s", getPlanDisplayName(req.Plan), getPeriodDisplayName(period)),
		},
	}

	qrisResp, err := generateQRIS(config, accessToken, qrisReq)
	if err != nil {
		fmt.Printf("Doku QRIS generation error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat QRIS. Silakan coba lagi."})
		return
	}

	// Store pending invoice in database
	var subscription database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&subscription)

	expiresAt := time.Now().Add(30 * time.Minute)
	invoiceNumber := fmt.Sprintf("INV-%s", referenceNo)

	invoice := database.Invoice{
		SubscriptionID: subscription.ID,
		InvoiceNumber:  invoiceNumber,
		Amount:         totalAmount,
		Status:         "pending",
		DueDate:        expiresAt,
		PaymentRef:     referenceNo,
		BillingPeriod:  req.BillingPeriod,
	}
	h.db.Create(&invoice)

	fmt.Printf("QRIS generated for tenant %s, plan %s (%s), amount Rp %.0f, ref: %s\n",
		tenantID, req.Plan, req.BillingPeriod, totalAmount, referenceNo)

	c.JSON(http.StatusOK, gin.H{
		"data": QRISSubscriptionResponse{
			QRContent:   qrisResp.QRContent,
			QRImageURL:  qrisResp.QRUrl,
			Amount:      totalAmount,
			BaseAmount:  basePrice,
			AdminFee:    adminFee,
			ExpiresAt:   expiresAt,
			ReferenceNo: referenceNo,
			Plan:        req.Plan,
			Period:      req.BillingPeriod,
		},
	})
}

// CheckQRISStatus checks the payment status of a QRIS code
func (h *Handler) CheckQRISStatus(c *gin.Context) {
	reference := c.Param("reference")

	// Find invoice
	var invoice database.Invoice
	if err := h.db.Where("payment_ref = ?", reference).First(&invoice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Referensi pembayaran tidak ditemukan"})
		return
	}

	// If already paid, return immediately
	if invoice.Status == "paid" {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":     "paid",
				"paid_at":    invoice.PaidAt,
				"reference":  reference,
			},
		})
		return
	}

	// If expired, return
	if time.Now().After(invoice.DueDate) {
		invoice.Status = "expired"
		h.db.Save(&invoice)

		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "expired",
				"reference": reference,
			},
		})
		return
	}

	// Query Doku for live status
	config, err := getDokuConfig()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    invoice.Status,
				"reference": reference,
			},
		})
		return
	}

	accessToken, err := getB2BAccessToken(config)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    invoice.Status,
				"reference": reference,
			},
		})
		return
	}

	queryReq := DokuQueryRequest{
		OriginalPartnerReferenceNo: reference,
		ServiceCode:                "47",
	}

	queryResp, err := queryQRISStatus(config, accessToken, queryReq)
	if err != nil {
		fmt.Printf("Doku query error: %v\n", err)
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    invoice.Status,
				"reference": reference,
			},
		})
		return
	}

	// Check if paid (responseCode "00" = success)
	if queryResp.LatestTransactionStatus == "00" {
		// Payment confirmed!
		invoice.Status = "paid"
		invoice.PaidAt = timePtr(time.Now())
		h.db.Save(&invoice)

		// Parse plan and period from reference
		plan, period := parsePlanFromReference(reference)
		if plan != "" {
			// Get subscription to find tenant_id
			var sub database.Subscription
			if err := h.db.Where("id = ?", invoice.SubscriptionID).First(&sub).Error; err == nil {
				periodMonths := GetPeriodMonths(BillingPeriod(period))
				if err := h.upgradeSubscription(sub.TenantID.String(), plan, periodMonths); err != nil {
					fmt.Printf("Failed to upgrade subscription from status check: %v\n", err)
				} else {
					fmt.Printf("Subscription upgraded via status check for tenant %s\n", sub.TenantID)
					// Record affiliate commission
					pricing := PlanPrices[plan]
					basePrice := pricing.GetPrice(BillingPeriod(period))
					h.recordAffiliateCommission(sub.TenantID.String(), plan, basePrice, invoice.ID)
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "paid",
				"paid_at":   invoice.PaidAt,
				"reference": reference,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"status":    "pending",
			"reference": reference,
		},
	})
}

// --- Doku Webhook ---

// DokuWebhookNotification represents the QRIS webhook payload from Doku
type DokuWebhookNotification struct {
	OriginalPartnerReferenceNo string                 `json:"originalPartnerReferenceNo"`
	OriginalReferenceNo        string                 `json:"originalReferenceNo"`
	LatestTransactionStatus    string                 `json:"latestTransactionStatus"`
	TransactionStatusDesc      string                 `json:"transactionStatusDesc"`
	Amount                     DokuAmount             `json:"amount"`
	AdditionalInfo             map[string]interface{} `json:"additionalInfo"`
}

// DokuVAWebhookNotification represents the VA payment notification payload from Doku
// VA notifications are only sent on successful payment — receiving one implies success.
type DokuVAWebhookNotification struct {
	PartnerServiceID string                 `json:"partnerServiceId"`
	CustomerNo       string                 `json:"customerNo"`
	VirtualAccountNo string                 `json:"virtualAccountNo"`
	TrxID            string                 `json:"trxId"`
	PaymentRequestID string                 `json:"paymentRequestId"`
	PaidAmount       DokuAmount             `json:"paidAmount"`
	TrxDateTime      string                 `json:"trxDateTime"`
	AdditionalInfo   map[string]interface{} `json:"additionalInfo"`
}

// DokuWebhook handles Doku payment notifications (QRIS + VA)
func (h *Handler) DokuWebhook(c *gin.Context) {
	// 1. Get Headers
	signature := c.GetHeader("X-SIGNATURE")
	timestamp := c.GetHeader("X-TIMESTAMP")
	authHeader := c.GetHeader("Authorization") // Bearer <token>

	if signature == "" || timestamp == "" || authHeader == "" {
		fmt.Println("Doku Webhook Error: Missing required headers")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required headers"})
		return
	}

	// 2. Read Raw Body
	bodyBytes, err := c.GetRawData()
	if err != nil {
		fmt.Printf("Doku Webhook Error: Failed to read body: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}
	bodyString := string(bodyBytes)

	// Log for debugging
	fmt.Printf("Doku Webhook Received:\nHeaders: Sig=%s, Time=%s\nBody: %s\n", signature, timestamp, bodyString)

	// 3. Verify Signature
	config, err := getDokuConfig()
	if err != nil {
		fmt.Printf("Doku Webhook Error: Config missing: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal configuration error"})
		return
	}

	// Extract token (remove "Bearer " prefix)
	accessToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Generate expected signature
	// Note: The endpoint path must match exactly what Doku sees. 
	// Usually it is the path from the domain root, e.g. /api/v1/webhook/doku
	endpointPath := "/api/v1/webhook/doku" 
	
	expectedSignature := generateSymmetricSignature(
		config.SecretKey,
		"POST",
		endpointPath,
		accessToken,
		bodyString,
		timestamp,
	)

	if signature != expectedSignature {
		fmt.Printf("Doku Webhook Error: Invalid signature.\nExpected: %s\nGot:      %s\nEndpoint: %s\n", expectedSignature, signature, endpointPath)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid signature"})
		return
	}

	// 4. Detect payload type: VA or QRIS
	// Parse into a generic map first to detect which type of notification this is
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawPayload); err != nil {
		fmt.Printf("Doku Webhook Error: JSON parse error: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	// VA notifications contain "trxId" and "virtualAccountNo" fields
	// QRIS notifications contain "originalPartnerReferenceNo" and "latestTransactionStatus"
	if _, hasVATrxID := rawPayload["trxId"]; hasVATrxID {
		// --- VA Payment Notification ---
		h.handleVAWebhook(c, bodyBytes)
		return
	}

	// --- QRIS Payment Notification (existing logic) ---
	var notification DokuWebhookNotification
	if err := json.Unmarshal(bodyBytes, &notification); err != nil {
		fmt.Printf("Doku Webhook Error: QRIS JSON parse error: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	fmt.Printf("Doku Webhook QRIS: ref=%s, status=%s, desc=%s\n",
		notification.OriginalPartnerReferenceNo,
		notification.LatestTransactionStatus,
		notification.TransactionStatusDesc,
	)

	referenceNo := notification.OriginalPartnerReferenceNo

	// Find invoice by reference
	var invoice database.Invoice
	if err := h.db.Where("payment_ref = ?", referenceNo).First(&invoice).Error; err != nil {
		fmt.Printf("Doku Webhook: Invoice not found for ref %s\n", referenceNo)
		c.JSON(http.StatusOK, gin.H{"message": "OK"}) 
		return
	}

	// Handle based on transaction status
	switch notification.LatestTransactionStatus {
	case "00": // Success
		if invoice.Status == "paid" {
			c.JSON(http.StatusOK, gin.H{"message": "OK"})
			return
		}
		h.processSuccessfulPayment(&invoice, referenceNo)

	case "05", "06": // Pending / In Progress
		invoice.Status = "pending"
		h.db.Save(&invoice)

	default: // Failed / Expired
		invoice.Status = "failed"
		h.db.Save(&invoice)
	}

	c.JSON(http.StatusOK, gin.H{"message": "OK"})
}

// handleVAWebhook handles DOKU VA payment notifications
// VA notifications are only sent on successful payment, so receiving one implies success.
func (h *Handler) handleVAWebhook(c *gin.Context, bodyBytes []byte) {
	var vaNotification DokuVAWebhookNotification
	if err := json.Unmarshal(bodyBytes, &vaNotification); err != nil {
		fmt.Printf("Doku VA Webhook Error: JSON parse error: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	fmt.Printf("Doku VA Webhook: trxId=%s, vaNo=%s, paidAmount=%s %s\n",
		vaNotification.TrxID,
		vaNotification.VirtualAccountNo,
		vaNotification.PaidAmount.Value,
		vaNotification.PaidAmount.Currency,
	)

	// Use trxId as the reference — this matches Invoice.PaymentRef
	referenceNo := vaNotification.TrxID

	// Find invoice by reference
	var invoice database.Invoice
	if err := h.db.Where("payment_ref = ?", referenceNo).First(&invoice).Error; err != nil {
		fmt.Printf("Doku VA Webhook: Invoice not found for trxId %s\n", referenceNo)
		c.JSON(http.StatusOK, gin.H{"message": "OK"})
		return
	}

	if invoice.Status == "paid" {
		// Already processed — idempotent
		fmt.Printf("Doku VA Webhook: Invoice %s already paid, skipping\n", referenceNo)
		c.JSON(http.StatusOK, gin.H{"message": "OK"})
		return
	}

	// VA notification = payment success
	h.processSuccessfulPayment(&invoice, referenceNo)

	c.JSON(http.StatusOK, gin.H{"message": "OK"})
}

// processSuccessfulPayment marks an invoice as paid, upgrades the subscription,
// records affiliate commission, and sends a payment success email.
func (h *Handler) processSuccessfulPayment(invoice *database.Invoice, referenceNo string) {
	invoice.Status = "paid"
	invoice.PaidAt = timePtr(time.Now())
	h.db.Save(invoice)

	// Get subscription for tenant_id
	var subscription database.Subscription
	if err := h.db.Where("id = ?", invoice.SubscriptionID).First(&subscription).Error; err != nil {
		fmt.Printf("Doku Webhook: Subscription not found for invoice %s\n", invoice.ID)
		return
	}

	// Parse plan and period from reference
	plan, period := parsePlanFromReference(referenceNo)
	if plan == "" {
		fmt.Printf("Doku Webhook: Could not parse plan from reference %s\n", referenceNo)
		return
	}

	// Upgrade subscription
	periodMonths := GetPeriodMonths(BillingPeriod(period))
	if err := h.upgradeSubscription(subscription.TenantID.String(), plan, periodMonths); err != nil {
		fmt.Printf("Doku Webhook: Failed to upgrade subscription: %v\n", err)
	} else {
		fmt.Printf("Doku Webhook: Successfully upgraded tenant %s to %s (%s)\n",
			subscription.TenantID, plan, period)

		// Handle promo code usage and affiliate linking
		if invoice.PromoCodeID != nil {
			h.processPromoUsage(invoice, subscription.TenantID.String())
		}

		// Record affiliate commission (uses invoice.Amount which is already discounted)
		h.recordAffiliateCommission(subscription.TenantID.String(), plan, invoice.Amount, invoice.ID)

		// Send payment success email (async)
		go func() {
			var tenant database.Tenant
			if err := h.db.Where("id = ?", subscription.TenantID).First(&tenant).Error; err != nil {
				fmt.Printf("Doku Webhook: Failed to get tenant for email: %v\n", err)
				return
			}

			var user database.User
			if err := h.db.Where("tenant_id = ? AND role = ?", subscription.TenantID, "owner").First(&user).Error; err != nil {
				fmt.Printf("Doku Webhook: Failed to get user for email: %v\n", err)
				return
			}

			emailService := email.NewEmailService()
			if !emailService.IsConfigured() {
				fmt.Println("Doku Webhook: Email service not configured, skipping notification")
				return
			}

			// Re-fetch subscription to get updated period end after upgrade
			var updatedSub database.Subscription
			if err := h.db.Where("id = ?", subscription.ID).First(&updatedSub).Error; err != nil {
				fmt.Printf("Doku Webhook: Failed to re-fetch subscription for email: %v\n", err)
				return
			}

			expiryDate := updatedSub.CurrentPeriodEnd.Format("02 January 2006")

			err := emailService.SendPaymentSuccessEmail(
				user.Email,
				user.Name,
				tenant.Name,
				getPlanDisplayName(plan),
				period,
				invoice.InvoiceNumber,
				invoice.Amount,
				expiryDate,
			)

			if err != nil {
				fmt.Printf("Doku Webhook: Failed to send payment success email: %v\n", err)
			} else {
				fmt.Printf("Doku Webhook: Payment success email sent to %s\n", user.Email)
			}
		}()
	}
}

// processPromoUsage records promo usage, increments usage count, and creates affiliate link if needed
func (h *Handler) processPromoUsage(invoice *database.Invoice, tenantID string) {
	if invoice.PromoCodeID == nil {
		return
	}

	// Create usage record
	tenantUUID, _ := uuid.Parse(tenantID)
	usage := database.PromoCodeUsage{
		PromoCodeID:    *invoice.PromoCodeID,
		TenantID:       tenantUUID,
		InvoiceID:      invoice.ID,
		DiscountAmount: invoice.DiscountAmount,
	}
	if err := h.db.Create(&usage).Error; err != nil {
		fmt.Printf("Doku Webhook: Failed to record promo usage: %v\n", err)
		return
	}

	// Increment promo current_uses
	h.db.Model(&database.PromoCode{}).Where("id = ?", invoice.PromoCodeID).
		UpdateColumn("current_uses", gorm.Expr("current_uses + 1"))

	fmt.Printf("Doku Webhook: Recorded promo usage for tenant %s, promo %s\n", tenantID, invoice.PromoCodeID)

	// If promo has referral code, create AffiliateTenant link
	var promo database.PromoCode
	if err := h.db.Where("id = ?", invoice.PromoCodeID).First(&promo).Error; err != nil {
		return
	}

	if promo.ReferralCode != nil && *promo.ReferralCode != "" {
		// Check if tenant already has an affiliate (skip if already linked)
		var existingAffiliate database.AffiliateTenant
		if err := h.db.Where("tenant_id = ?", tenantID).First(&existingAffiliate).Error; err == nil {
			fmt.Printf("Doku Webhook: Tenant %s already has affiliate, skipping\n", tenantID)
			return
		}

		// Find the affiliator by referral code
		var affiliator database.PortalUser
		if err := h.db.Where("referral_code = ? AND is_active = true AND role = 'affiliator'", *promo.ReferralCode).First(&affiliator).Error; err != nil {
			fmt.Printf("Doku Webhook: Affiliator not found for referral code %s: %v\n", *promo.ReferralCode, err)
			return
		}

		// Create AffiliateTenant link
		affiliateTenant := database.AffiliateTenant{
			PortalUserID: affiliator.ID,
			TenantID:     tenantUUID,
		}
		if err := h.db.Create(&affiliateTenant).Error; err != nil {
			fmt.Printf("Doku Webhook: Failed to create affiliate link: %v\n", err)
		} else {
			fmt.Printf("Doku Webhook: Auto-linked tenant %s to affiliator %s via promo code\n", tenantID, affiliator.Name)
		}
	}
}

// WebhookVerify handles GET requests for webhook URL verification
func (h *Handler) WebhookVerify(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "OK",
		"message": "Webhook endpoint is active",
		"service": "warungin",
	})
}

// --- Subscription Upgrade ---

// upgradeSubscription upgrades a tenant's subscription with the given billing period
func (h *Handler) upgradeSubscription(tenantID string, plan string, periodMonths int) error {
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
			MaxTransactionsDaily:   0,
			MaxTransactionsMonthly: 0,
			MaxOutlets:             1,
			DataRetentionDays:      365,
		},
		"bisnis": {
			MaxUsers:               10,
			MaxProducts:            0,
			MaxTransactionsDaily:   0,
			MaxTransactionsMonthly: 0,
			MaxOutlets:             3,
			DataRetentionDays:      365 * 3,
		},
		"enterprise": {
			MaxUsers:               0,
			MaxProducts:            0,
			MaxTransactionsDaily:   0,
			MaxTransactionsMonthly: 0,
			MaxOutlets:             0,
			DataRetentionDays:      0,
		},
	}

	limits, ok := planLimits[plan]
	if !ok {
		return fmt.Errorf("invalid plan: %s", plan)
	}

	// Migrate orphaned data to first outlet
	var firstOutlet database.Outlet
	if err := h.db.Where("tenant_id = ?", tenantID).Order("created_at ASC").First(&firstOutlet).Error; err == nil {
		h.db.Model(&database.Product{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		h.db.Model(&database.RawMaterial{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		h.db.Model(&database.User{}).
			Where("tenant_id = ? AND outlet_id IS NULL", tenantID).
			Update("outlet_id", firstOutlet.ID)

		fmt.Printf("Migrated orphaned data to outlet %s for tenant %s\n", firstOutlet.ID, tenantID)
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
	subscription.CurrentPeriodEnd = time.Now().AddDate(0, periodMonths, 0)
	subscription.BillingPeriod = string(getBillingPeriodFromMonths(periodMonths))
	subscription.CancelledAt = nil
	subscription.AutoRenew = true

	return h.db.Save(&subscription).Error
}

// --- Affiliate Commission ---

// recordAffiliateCommission records commission for affiliate if tenant has a referrer
func (h *Handler) recordAffiliateCommission(tenantID string, plan string, basePrice float64, invoiceID uuid.UUID) {
	// Check if tenant has an affiliate
	var affTenant database.AffiliateTenant
	if err := h.db.Where("tenant_id = ?", tenantID).First(&affTenant).Error; err != nil {
		return
	}

	tenantUUID, _ := uuid.Parse(tenantID)

	// Check if commission already exists for this invoice
	var existingEarning database.AffiliateEarning
	if invoiceID != uuid.Nil {
		if err := h.db.Where("tenant_id = ? AND invoice_id = ?", tenantUUID, invoiceID).First(&existingEarning).Error; err == nil {
			fmt.Printf("Commission already exists for invoice %s, skipping\n", invoiceID)
			return
		}
	} else {
		thisMonth := time.Now().Format("2006-01")
		if err := h.db.Where("tenant_id = ? AND subscription_plan = ? AND created_at >= ?", tenantUUID, plan, thisMonth+"-01").First(&existingEarning).Error; err == nil {
			fmt.Printf("Commission already exists for tenant %s this month, skipping\n", tenantID)
			return
		}
	}

	if basePrice == 0 {
		fmt.Printf("No price for plan %s, skipping commission\n", plan)
		return
	}

	// Calculate 10% commission on base price (before PPN)
	commissionRate := 10.0
	commissionAmount := basePrice * (commissionRate / 100)

	earning := database.AffiliateEarning{
		PortalUserID:      affTenant.PortalUserID,
		TenantID:          tenantUUID,
		InvoiceID:         invoiceID,
		SubscriptionPlan:  plan,
		SubscriptionPrice: basePrice,
		CommissionRate:    commissionRate,
		CommissionAmount:  commissionAmount,
		Status:            "pending",
	}

	if err := h.db.Create(&earning).Error; err != nil {
		fmt.Printf("Failed to create affiliate earning: %v\n", err)
		return
	}

	h.db.Model(&database.PortalUser{}).
		Where("id = ?", affTenant.PortalUserID).
		UpdateColumn("pending_payout", gorm.Expr("pending_payout + ?", commissionAmount))

	fmt.Printf("Recorded affiliate commission: Rp %.0f for affiliator %s\n", commissionAmount, affTenant.PortalUserID)
}

// RecordMissingCommissions checks for tenants with affiliates on paid plans without commission
func (h *Handler) RecordMissingCommissions() {
	fmt.Println("Checking for missing affiliate commissions...")

	var affTenants []database.AffiliateTenant
	h.db.Preload("Tenant").Preload("Tenant.Subscription").Find(&affTenants)

	for _, affTenant := range affTenants {
		if affTenant.Tenant.Subscription == nil {
			continue
		}

		plan := affTenant.Tenant.Subscription.Plan
		if plan == "gratis" || plan == "" {
			continue
		}

		var existingEarning database.AffiliateEarning
		if err := h.db.Where("tenant_id = ? AND subscription_plan = ?", affTenant.TenantID, plan).First(&existingEarning).Error; err != nil {
			fmt.Printf("Creating missing commission for tenant %s on plan %s\n", affTenant.TenantID, plan)
			pricing := PlanPrices[plan]
			basePrice := pricing.Monthly // Use monthly as default for historical
			h.recordAffiliateCommission(affTenant.TenantID.String(), plan, basePrice, uuid.Nil)
		}
	}

	fmt.Println("Finished checking for missing commissions")
}

// --- Helper Functions ---

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

func getPeriodDisplayName(period BillingPeriod) string {
	names := map[BillingPeriod]string{
		BillingMonthly:   "Bulanan",
		BillingQuarterly: "3 Bulan",
		BillingYearly:    "Tahunan",
	}
	if name, ok := names[period]; ok {
		return name
	}
	return string(period)
}

func getBillingPeriodFromMonths(months int) BillingPeriod {
	switch months {
	case 3:
		return BillingQuarterly
	case 12:
		return BillingYearly
	default:
		return BillingMonthly
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func generateTransactionID() string {
	return uuid.New().String()
}

// parsePlanFromReference extracts plan and period from reference number
// Format: WSUB-{tenant_prefix}-{plan}-{period_initial}-{timestamp}
// Example: WSUB-b89beb65-bisnis-q-1769014518
func parsePlanFromReference(reference string) (plan string, period string) {
	knownPlans := []string{"enterprise", "bisnis", "pemula", "gratis"}
	periodMap := map[string]string{
		"m": "monthly",
		"q": "quarterly",
		"y": "yearly",
	}

	for _, p := range knownPlans {
		idx := -1
		// Find the plan name in the reference
		for i := 0; i <= len(reference)-len(p); i++ {
			if reference[i:i+len(p)] == p {
				// Check boundaries
				beforeOK := i == 0 || reference[i-1] == '-'
				afterOK := i+len(p) == len(reference) || reference[i+len(p)] == '-'
				if beforeOK && afterOK {
					idx = i
					break
				}
			}
		}
		if idx != -1 {
			plan = p
			// Try to find period initial after plan
			after := reference[idx+len(p):]
			if len(after) >= 2 && after[0] == '-' {
				periodInitial := string(after[1])
				if fullPeriod, ok := periodMap[periodInitial]; ok {
					period = fullPeriod
				}
			}
			if period == "" {
				period = "monthly"
			}
			return
		}
	}
	return "", ""
}

// DokuNotifyBody is used to decode raw webhook body for signature verification
type DokuNotifyBody struct {
	Body json.RawMessage
}

// --- VA Subscription Payment ---

// CreateVASubscriptionRequest is the request to generate a VA for subscription payment
type CreateVASubscriptionRequest struct {
	Plan          string  `json:"plan" binding:"required"`
	BillingPeriod string  `json:"billing_period" binding:"required"`
	Email         string  `json:"email" binding:"required"`
	BankCode      string  `json:"bank_code" binding:"required"` // "mandiri", "bni", "bri"
	PromoCode     *string `json:"promo_code"`                    // Optional promo code
}

// VASubscriptionResponse is returned to the frontend with VA data
type VASubscriptionResponse struct {
	VANumber       string    `json:"va_number"`
	BankName       string    `json:"bank_name"`
	BankCode       string    `json:"bank_code"`
	Amount         float64   `json:"amount"`
	BaseAmount     float64   `json:"base_amount"`
	AdminFee       float64   `json:"admin_fee"`
	DiscountAmount float64   `json:"discount_amount"`
	OriginalAmount float64   `json:"original_amount"`
	PromoCode      string    `json:"promo_code,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	ReferenceNo    string    `json:"reference_no"`
	Plan           string    `json:"plan"`
	Period         string    `json:"billing_period"`
	Instructions   []string  `json:"instructions"`
}

// ValidatePromoRequest is the request to validate a promo code
type ValidatePromoRequest struct {
	PromoCode string `json:"promo_code" binding:"required"`
	Plan      string `json:"plan" binding:"required"`
	Period    string `json:"billing_period" binding:"required"`
}

// ValidatePromoCode validates a promo code and returns the discount preview
func (h *Handler) ValidatePromoCode(c *gin.Context) {
	var req ValidatePromoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	promoCode := strings.ToUpper(strings.TrimSpace(req.PromoCode))

	promo, errMsg := h.validatePromo(promoCode, tenantID, req.Plan)
	if errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return
	}

	// Calculate discount preview
	pricing, ok := PlanPrices[req.Plan]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket tidak valid"})
		return
	}

	period := BillingPeriod(req.Period)
	basePrice := pricing.GetPrice(period)
	if basePrice == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket ini gratis"})
		return
	}

	discountAmount := calculateDiscount(promo, basePrice)
	finalAmount := basePrice - discountAmount + AdminFee

	c.JSON(http.StatusOK, gin.H{
		"valid":           true,
		"promo_code":      promo.FullCode,
		"discount_type":   promo.DiscountType,
		"discount_value":  promo.DiscountValue,
		"base_price":      basePrice,
		"discount_amount": discountAmount,
		"admin_fee":       AdminFee,
		"final_amount":    finalAmount,
		"has_referral":    promo.ReferralCode != nil && *promo.ReferralCode != "",
	})
}

// validatePromo validates a promo code against all business rules
// Returns the promo and an error message (empty string if valid)
func (h *Handler) validatePromo(code string, tenantID string, plan string) (*database.PromoCode, string) {
	var promo database.PromoCode
	if err := h.db.Where("full_code = ? AND is_active = true", code).First(&promo).Error; err != nil {
		return nil, "Kode promo tidak valid atau tidak aktif"
	}

	now := time.Now()
	if now.Before(promo.ValidFrom) || now.After(promo.ValidUntil) {
		return nil, "Kode promo sudah tidak berlaku"
	}

	if promo.MaxUses != nil && promo.CurrentUses >= *promo.MaxUses {
		return nil, "Kode promo sudah mencapai batas penggunaan"
	}

	// Check one-time per tenant
	var usageCount int64
	h.db.Model(&database.PromoCodeUsage{}).Where("promo_code_id = ? AND tenant_id = ?", promo.ID, tenantID).Count(&usageCount)
	if usageCount > 0 {
		return nil, "Anda sudah pernah menggunakan kode promo ini"
	}

	// Check applicable plans
	if promo.ApplicablePlans != "" {
		applicable := false
		for _, p := range strings.Split(promo.ApplicablePlans, ",") {
			if strings.TrimSpace(p) == plan {
				applicable = true
				break
			}
		}
		if !applicable {
			return nil, "Kode promo tidak berlaku untuk paket ini"
		}
	}

	// Check affiliate conflict
	if promo.ReferralCode != nil && *promo.ReferralCode != "" {
		var existingAffiliate database.AffiliateTenant
		if err := h.db.Where("tenant_id = ?", tenantID).First(&existingAffiliate).Error; err == nil {
			// Tenant already has an affiliate — check if it's the same referral
			var affiliator database.PortalUser
			if err := h.db.Where("id = ?", existingAffiliate.PortalUserID).First(&affiliator).Error; err == nil {
				if affiliator.ReferralCode != *promo.ReferralCode {
					return nil, "Anda sudah terdaftar dengan kode referral lain"
				}
			}
		}
	}

	return &promo, ""
}

// calculateDiscount calculates the discount amount based on promo type
func calculateDiscount(promo *database.PromoCode, basePrice float64) float64 {
	if promo.DiscountType == "percentage" {
		return basePrice * promo.DiscountValue / 100
	}
	// Fixed discount — cap at base price
	if promo.DiscountValue > basePrice {
		return basePrice
	}
	return promo.DiscountValue
}

// CreateSubscriptionVA generates a Doku Virtual Account for subscription payment
func (h *Handler) CreateSubscriptionVA(c *gin.Context) {
	var req CreateVASubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	// Validate bank code
	bankConfig, ok := VABanks[req.BankCode]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bank tidak valid. Pilih: mandiri, bni, bri"})
		return
	}
	fmt.Printf("DEBUG VA: BankCode=%s, PartnerID=%s, ChannelID=%s\n", req.BankCode, bankConfig.PartnerServiceID, bankConfig.ChannelID)

	// Validate billing period
	if !ValidBillingPeriod(req.BillingPeriod) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Periode billing tidak valid. Pilih: monthly, quarterly, yearly"})
		return
	}

	// Check plan price
	pricing, ok := PlanPrices[req.Plan]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket tidak valid"})
		return
	}

	period := BillingPeriod(req.BillingPeriod)
	basePrice := pricing.GetPrice(period)

	if basePrice == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket ini tidak memerlukan pembayaran"})
		return
	}

	// Calculate admin fee (flat Rp 2,500)
	adminFee := AdminFee
	originalAmount := basePrice + adminFee

	// Handle promo code
	var promoCodeID *uuid.UUID
	var discountAmount float64
	var appliedPromo *database.PromoCode
	var promoCodeStr string

	if req.PromoCode != nil && *req.PromoCode != "" {
		code := strings.ToUpper(strings.TrimSpace(*req.PromoCode))
		promo, errMsg := h.validatePromo(code, tenantID, req.Plan)
		if errMsg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
		appliedPromo = promo
		discountAmount = calculateDiscount(promo, basePrice)
		promoCodeID = &promo.ID
		promoCodeStr = promo.FullCode
	}

	totalAmount := originalAmount - discountAmount

	// Get Doku config
	config, err := getDokuConfig()
	if err != nil {
		fmt.Printf("Doku VA config error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Payment gateway belum dikonfigurasi"})
		return
	}

	// Get B2B access token
	accessToken, err := getB2BAccessToken(config)
	if err != nil {
		fmt.Printf("Doku VA token error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghubungi payment gateway"})
		return
	}

	// Create reference number (trxId)
	trxID := fmt.Sprintf("WSUB-%s-%s-%s-%d", tenantID[:8], req.Plan, req.BillingPeriod[:1], time.Now().Unix())

	// Generate customer number
	// Mandiri VA format: 8808 + 8 digits MerchantCode + CustomerNo (Total max ?)
	// Actually for Doku Aggregator:
	// PartnerID is fixed (e.g. 86188)
	// VirtualAccountNo = PartnerID + CustomerNo
	
	// Doku Requirement: PartnerServiceID must be 8 chars for Mandiri (left padded with space if needed)
	// But our screenshot showed "86188" without spaces.
	// Let's follow the error "Invalid Field Format {customerNo, partnerServiceId, virtualAccountNo}"
	// This usually means length or content type (numeric) is wrong.
	
	// Fix 1: PartnerServiceID must be 8 chars for Mandiri?
	// Actually, for aggregator, it might be just the 5 digits.
	// BUT, CustomerNo MUST be numeric only.
	// Our previous CustomerNo was "7981fe4c20652" (alphanumeric due to hex/uuid parts).
	// Mandiri VA (and most VAs) requires NUMERIC ONLY.
	
	// Generate strictly numeric CustomerNo
	// Use: Bank Prefix + last 7 digits of timestamp/modulo to keep total length short (approx 12-14 digits total VA)
	// This ensures we stay within standard 16-digit limit even with prefixes.
	customerNo := fmt.Sprintf("%s%07d", bankConfig.CustomerPrefix, time.Now().UnixNano()%10000000)

	// Full VA number = partnerServiceId + customerNo
	vaNumber := bankConfig.PartnerServiceID + customerNo

	// Expiry: 24 hours for VA
	expiresAt := time.Now().Add(24 * time.Hour)
	expiryISO := expiresAt.In(jakartaLoc).Format("2006-01-02T15:04:05+07:00")

	// Configuration for VirtualAccountConfig based on bank samples
	var vaConfig *DokuVAConfig
	switch req.BankCode {
	case "mandiri":
		// Mandiri worked without this field previously. Keeping it nil to restore functionality.
		vaConfig = nil // Or {ReusableStatus: true} based on sample, but nil is safer if it worked.
	case "bni":
		vaConfig = &DokuVAConfig{ReusableStatus: false} // Sample says false
	case "bri":
		vaConfig = &DokuVAConfig{ReusableStatus: true} // Sample says true
	default:
		vaConfig = &DokuVAConfig{ReusableStatus: true} // Default
	}

	// Build VA request
	vaReq := DokuVARequest{
		PartnerServiceID:      bankConfig.PartnerServiceID, // Keep as is for now, fix CustomerNo first
		CustomerNo:            customerNo,
		VirtualAccountNo:      vaNumber,
		VirtualAccountName:    fmt.Sprintf("Warungin %s", getPlanDisplayName(req.Plan)),
		TrxID:                 trxID,
		TotalAmount:           DokuAmount{Value: fmt.Sprintf("%.2f", totalAmount), Currency: "IDR"},
		VirtualAccountTrxType: "C",       // Closed payment (Moved to root)
		ExpiredDate:           expiryISO, // Moved to root
		AdditionalInfo: &DokuVAAdditional{
			Channel:              bankConfig.ChannelID,
			VirtualAccountConfig: vaConfig,
		},
	}

	// Previously tried omitting VirtualAccountNo for DGPC, but failed with "Transaction Not Permitted".
	// Reverting to sending explicit VirtualAccountNo.
	// Ensure PartnerServiceID and CustomerNo are correct.

	vaResp, err := generateVA(config, accessToken, vaReq)
	if err != nil {
		fmt.Printf("Doku VA generation error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat Virtual Account. Silakan coba lagi."})
		return
	}

	// Extract VA number from response (use response value if available)
	finalVANumber := vaNumber
	if vaResp.VirtualAccountData != nil && vaResp.VirtualAccountData.VirtualAccountNo != "" {
		finalVANumber = vaResp.VirtualAccountData.VirtualAccountNo
	}

	// Store pending invoice in database
	var subscription database.Subscription
	h.db.Where("tenant_id = ?", tenantID).First(&subscription)

	invoiceNumber := fmt.Sprintf("INV-%s", trxID)
	paymentMethod := "va_" + req.BankCode

	invoice := database.Invoice{
		SubscriptionID: subscription.ID,
		InvoiceNumber:  invoiceNumber,
		Amount:         totalAmount,
		OriginalAmount: originalAmount,
		DiscountAmount: discountAmount,
		PromoCodeID:    promoCodeID,
		Status:         "pending",
		DueDate:        expiresAt,
		PaymentRef:     trxID,
		BillingPeriod:  req.BillingPeriod,
		PaymentMethod:  paymentMethod,
		VANumber:       finalVANumber,
	}
	_ = appliedPromo // Used in response below
	h.db.Create(&invoice)

	// Generate payment instructions
	instructions := getVAInstructions(bankConfig.Code, finalVANumber)

	fmt.Printf("VA generated for tenant %s, bank %s, VA: %s, amount Rp %.0f, ref: %s\n",
		tenantID, bankConfig.DisplayName, finalVANumber, totalAmount, trxID)

	c.JSON(http.StatusOK, gin.H{
		"data": VASubscriptionResponse{
			VANumber:       finalVANumber,
			BankName:       bankConfig.DisplayName,
			BankCode:       req.BankCode,
			Amount:         totalAmount,
			BaseAmount:     basePrice,
			AdminFee:       adminFee,
			DiscountAmount: discountAmount,
			OriginalAmount: originalAmount,
			PromoCode:      promoCodeStr,
			ExpiresAt:      expiresAt,
			ReferenceNo:    trxID,
			Plan:           req.Plan,
			Period:         req.BillingPeriod,
			Instructions:   instructions,
		},
	})
}

// CheckVAStatus checks the payment status of a Virtual Account
func (h *Handler) CheckVAStatus(c *gin.Context) {
	reference := c.Param("reference")

	// Find invoice
	var invoice database.Invoice
	if err := h.db.Where("payment_ref = ?", reference).First(&invoice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Referensi pembayaran tidak ditemukan"})
		return
	}

	// If already paid, return immediately
	if invoice.Status == "paid" {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "paid",
				"paid_at":   invoice.PaidAt,
				"reference": reference,
			},
		})
		return
	}

	// If expired, return
	if time.Now().After(invoice.DueDate) {
		invoice.Status = "expired"
		h.db.Save(&invoice)

		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "expired",
				"reference": reference,
			},
		})
		return
	}

	// Query Doku for live status
	config, err := getDokuConfig()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "pending",
				"reference": reference,
			},
		})
		return
	}

	accessToken, err := getB2BAccessToken(config)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "pending",
				"reference": reference,
			},
		})
		return
	}

	// Extract bank from payment_method to get partnerServiceId
	bankCode := strings.TrimPrefix(invoice.PaymentMethod, "va_")
	bankConfig, ok := VABanks[bankCode]
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "pending",
				"reference": reference,
			},
		})
		return
	}

	// Parse customerNo from stored VA number
	customerNo := ""
	if len(invoice.VANumber) > len(bankConfig.PartnerServiceID) {
		customerNo = invoice.VANumber[len(bankConfig.PartnerServiceID):]
	}

	statusReq := DokuVAStatusRequest{
		PartnerServiceID: bankConfig.PartnerServiceID,
		CustomerNo:       customerNo,
		VirtualAccountNo: invoice.VANumber,
		TrxID:            reference,
	}

	statusResp, err := queryVAStatus(config, accessToken, statusReq)
	if err != nil {
		fmt.Printf("Doku VA status query error: %v\n", err)
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "pending",
				"reference": reference,
			},
		})
		return
	}

	// Check status based on PaymentFlagReason
	// Doku v1.0/transfer-va/status returns 200xxxx even for pending.
	// We must verify the actual status message.
	isPaid := false
	if statusResp.VirtualAccountData != nil {
		if reasonMap, ok := statusResp.VirtualAccountData.PaymentFlagReason.(map[string]interface{}); ok {
			if val, ok := reasonMap["english"].(string); ok && val == "Success" {
				isPaid = true
			}
		} else if reasonStr, ok := statusResp.VirtualAccountData.PaymentFlagReason.(string); ok {
			if reasonStr == "Success" {
				isPaid = true
			}
		}
	}

	// Double check response code is generally successful
	if isPaid && (strings.HasPrefix(statusResp.ResponseCode, "200")) {
		// Payment successful
		invoice.Status = "paid"
		invoice.PaidAt = timePtr(time.Now())
		h.db.Save(&invoice)

		// Parse plan and period from reference and upgrade
		plan, period := parsePlanFromReference(reference)
		if plan != "" {
			var sub database.Subscription
			if err := h.db.Where("id = ?", invoice.SubscriptionID).First(&sub).Error; err == nil {
				periodMonths := GetPeriodMonths(BillingPeriod(period))
				if err := h.upgradeSubscription(sub.TenantID.String(), plan, periodMonths); err != nil {
					fmt.Printf("Failed to upgrade subscription from VA status check: %v\n", err)
				} else {
					fmt.Printf("Subscription upgraded via VA status check for tenant %s\n", sub.TenantID)
					// Record affiliate commission
					pricing := PlanPrices[plan]
					basePrice := pricing.GetPrice(BillingPeriod(period))
					h.recordAffiliateCommission(sub.TenantID.String(), plan, basePrice, invoice.ID)

					// Send payment success email (async)
					go func() {
						var tenant database.Tenant
						if err := h.db.Where("id = ?", sub.TenantID).First(&tenant).Error; err != nil {
							return
						}
						var user database.User
						if err := h.db.Where("tenant_id = ? AND role = ?", sub.TenantID, "owner").First(&user).Error; err != nil {
							return
						}
						emailService := email.NewEmailService()
						if !emailService.IsConfigured() {
							return
						}
						expiryDate := sub.CurrentPeriodEnd.Format("02 January 2006")
						emailService.SendPaymentSuccessEmail(
							user.Email, user.Name, tenant.Name,
							getPlanDisplayName(plan), period,
							invoice.InvoiceNumber, invoice.Amount, expiryDate,
						)
					}()
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"status":    "paid",
				"paid_at":   invoice.PaidAt,
				"reference": reference,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"status":    "pending",
			"reference": reference,
		},
	})
}

// GetAvailablePaymentMethods returns the list of available payment methods
func (h *Handler) GetAvailablePaymentMethods(c *gin.Context) {
	methods := []gin.H{
		{
			"code":        "qris",
			"name":        "QRIS",
			"description": "Scan QR code untuk bayar dari aplikasi e-wallet atau mobile banking",
			"type":        "qris",
			"icon":        "qr-code",
		},
	}

	for code, bank := range VABanks {
		methods = append(methods, gin.H{
			"code":        "va_" + code,
			"name":        "Virtual Account " + bank.DisplayName,
			"description": "Transfer melalui ATM, mobile banking, atau internet banking " + bank.DisplayName,
			"type":        "va",
			"bank_code":   code,
			"icon":        "bank",
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": methods})
}
