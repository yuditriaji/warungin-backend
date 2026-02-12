package payment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// --- VA Bank Configuration ---

// VABankConfig holds configuration for each VA bank
type VABankConfig struct {
	Code             string // "MANDIRI", "BNI", "BRI"
	PartnerServiceID string // Bank-specific prefix
	DisplayName      string // User-facing bank name
}

// VABanks maps bank codes to their configurations
// PartnerServiceID must be 8 characters, left-padded with spaces
var VABanks = map[string]VABankConfig{
	"mandiri": {
		Code:             "MANDIRI",
		PartnerServiceID: "     008", // 5 spaces + 008
		DisplayName:      "Bank Mandiri",
	},
	"bni": {
		Code:             "BNI",
		PartnerServiceID: "     009", // 5 spaces + 009
		DisplayName:      "Bank BNI",
	},
	"bri": {
		Code:             "BRI",
		PartnerServiceID: "     002", // 5 spaces + 002
		DisplayName:      "Bank BRI",
	},
}

// --- VA Request/Response Structs ---

// DokuVARequest is the request body for creating a Virtual Account
type DokuVARequest struct {
	PartnerServiceID   string           `json:"partnerServiceId"`
	CustomerNo         string           `json:"customerNo"`
	VirtualAccountNo   string           `json:"virtualAccountNo"`
	VirtualAccountName string           `json:"virtualAccountName"`
	TrxID              string           `json:"trxId"`
	TotalAmount        DokuAmount       `json:"totalAmount"`
	AdditionalInfo     *DokuVAAdditional `json:"additionalInfo,omitempty"`
}

// DokuVAAdditional holds additional info for VA creation
type DokuVAAdditional struct {
	VirtualAccountTrxType     string `json:"virtualAccountTrxType,omitempty"` // "C" = Close Amount, "O" = Open Amount
	VirtualAccountExpiredDate string `json:"expiredDate,omitempty"`           // ISO8601 format
}

// DokuVAResponse is the response from VA creation
type DokuVAResponse struct {
	ResponseCode    string     `json:"responseCode"`
	ResponseMessage string     `json:"responseMessage"`
	VirtualAccountData *DokuVAData `json:"virtualAccountData,omitempty"`
}

// DokuVAData holds the VA data returned from creation
type DokuVAData struct {
	PartnerServiceID   string     `json:"partnerServiceId"`
	CustomerNo         string     `json:"customerNo"`
	VirtualAccountNo   string     `json:"virtualAccountNo"`
	VirtualAccountName string     `json:"virtualAccountName"`
	TrxID              string     `json:"trxId"`
	TotalAmount        DokuAmount `json:"totalAmount"`
	AdditionalInfo     map[string]interface{} `json:"additionalInfo,omitempty"`
}

// DokuVAStatusRequest is the request for checking VA payment status
type DokuVAStatusRequest struct {
	PartnerServiceID string `json:"partnerServiceId"`
	CustomerNo       string `json:"customerNo"`
	VirtualAccountNo string `json:"virtualAccountNo"`
	InquiryRequestID string `json:"inquiryRequestId,omitempty"`
}

// DokuVAStatusResponse is the response from VA status check
type DokuVAStatusResponse struct {
	ResponseCode    string           `json:"responseCode"`
	ResponseMessage string           `json:"responseMessage"`
	VirtualAccountData *DokuVAStatusData `json:"virtualAccountData,omitempty"`
}

// DokuVAStatusData holds VA status details
type DokuVAStatusData struct {
	PaymentFlagReason      string     `json:"paymentFlagReason"`
	PartnerServiceID       string     `json:"partnerServiceId"`
	CustomerNo             string     `json:"customerNo"`
	VirtualAccountNo       string     `json:"virtualAccountNo"`
	TrxID                  string     `json:"trxId"`
	PaidAmount             DokuAmount `json:"paidAmount"`
	BillAmount             DokuAmount `json:"billAmount"`
	AdditionalInfo         map[string]interface{} `json:"additionalInfo,omitempty"`
}

// --- VA API Functions ---

// generateVA calls Doku SNAP API to create a Virtual Account
func generateVA(config *DokuConfig, accessToken string, req DokuVARequest) (*DokuVAResponse, error) {
	endpointPath := "/virtual-accounts/bi-snap-va/v1.1/transfer-va/create-va"
	url := config.BaseURL + endpointPath

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal VA request: %v", err)
	}

	timestamp := jakartaTimestamp()
	externalID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Generate symmetric signature
	signature := generateSymmetricSignature(
		config.SecretKey,
		"POST",
		endpointPath,
		accessToken,
		string(reqJSON),
		timestamp,
	)

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create VA request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("X-PARTNER-ID", config.ClientID)
	httpReq.Header.Set("X-EXTERNAL-ID", externalID)
	httpReq.Header.Set("X-TIMESTAMP", timestamp)
	httpReq.Header.Set("X-SIGNATURE", signature)
// CreateSubscriptionVA generates a Doku Virtual Account for subscription payment
func (h *Handler) CreateSubscriptionVA(c *gin.Context) {
	// ... (code omitted)

	// Build VA request
	vaReq := DokuVARequest{
		PartnerServiceID:   bankConfig.PartnerServiceID,
		CustomerNo:         customerNo,
		VirtualAccountNo:   vaNumber,
		VirtualAccountName: fmt.Sprintf("Warungin %s", getPlanDisplayName(req.Plan)),
		TrxID:              trxID,
		TotalAmount: DokuAmount{
			Value:    fmt.Sprintf("%.2f", totalAmount),
			Currency: "IDR",
		},
		AdditionalInfo: &DokuVAAdditional{
			// Channel removed
			VirtualAccountTrxType:     "C", // Close Amount
			VirtualAccountExpiredDate: expiryISO,
		},
	}

	vaResp, err := generateVA(config, accessToken, vaReq)
	// ... (code omitted)
}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Doku VA API: %v", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read VA response body: %v", err)
	}

	fmt.Printf("Doku VA API Response [%d]: %s\n", resp.StatusCode, string(respBody))

	var vaResp DokuVAResponse
	if err := json.Unmarshal(respBody, &vaResp); err != nil {
		return nil, fmt.Errorf("failed to decode VA response: %v", err)
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("VA generation failed [%d]: %s - %s", resp.StatusCode, vaResp.ResponseCode, vaResp.ResponseMessage)
	}

	return &vaResp, nil
}

// queryVAStatus calls Doku SNAP API to check VA payment status
func queryVAStatus(config *DokuConfig, accessToken string, req DokuVAStatusRequest) (*DokuVAStatusResponse, error) {
	endpointPath := "/virtual-accounts/bi-snap-va/v1.1/transfer-va/status"
	url := config.BaseURL + endpointPath

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal VA status request: %v", err)
	}

	timestamp := jakartaTimestamp()
	externalID := fmt.Sprintf("%d", time.Now().UnixNano())

	signature := generateSymmetricSignature(
		config.SecretKey,
		"POST",
		endpointPath,
		accessToken,
		string(reqJSON),
		timestamp,
	)

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create VA status request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("X-PARTNER-ID", config.ClientID)
	httpReq.Header.Set("X-EXTERNAL-ID", externalID)
	httpReq.Header.Set("X-TIMESTAMP", timestamp)
	httpReq.Header.Set("X-SIGNATURE", signature)
	httpReq.Header.Set("CHANNEL-ID", "SDK")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Doku VA Status API: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read VA status response body: %v", err)
	}

	fmt.Printf("Doku VA Status Response [%d]: %s\n", resp.StatusCode, string(respBody))

	var statusResp DokuVAStatusResponse
	if err := json.Unmarshal(respBody, &statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode VA status response: %v", err)
	}

	return &statusResp, nil
}

// --- VA Payment Instructions ---

// getVAInstructions returns step-by-step payment instructions for each bank
func getVAInstructions(bankCode, vaNumber string) []string {
	switch bankCode {
	case "MANDIRI":
		return []string{
			"Buka aplikasi Mandiri Online / Livin' by Mandiri",
			"Pilih menu 'Bayar'",
			"Pilih 'Multipayment'",
			"Pilih penyedia jasa 'DOKU'",
			"Masukkan nomor Virtual Account: " + vaNumber,
			"Konfirmasi detail pembayaran dan jumlah",
			"Masukkan PIN untuk menyelesaikan pembayaran",
		}
	case "BNI":
		return []string{
			"Buka aplikasi BNI Mobile Banking",
			"Pilih menu 'Transfer'",
			"Pilih 'Virtual Account Billing'",
			"Masukkan nomor Virtual Account: " + vaNumber,
			"Konfirmasi detail pembayaran dan jumlah",
			"Masukkan password transaksi untuk menyelesaikan",
		}
	case "BRI":
		return []string{
			"Buka aplikasi BRImo / BRI Internet Banking",
			"Pilih menu 'Pembayaran'",
			"Pilih 'BRIVA'",
			"Masukkan nomor Virtual Account: " + vaNumber,
			"Konfirmasi detail pembayaran dan jumlah",
			"Masukkan PIN untuk menyelesaikan pembayaran",
		}
	default:
		return []string{
			"Login ke aplikasi mobile banking Anda",
			"Pilih menu 'Transfer' atau 'Pembayaran'",
			"Pilih 'Virtual Account'",
			"Masukkan nomor Virtual Account: " + vaNumber,
			"Konfirmasi dan selesaikan pembayaran",
		}
	}
}
