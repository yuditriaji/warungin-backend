package payment

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// DokuConfig holds Doku SNAP Adapter API configuration
type DokuConfig struct {
	ClientID   string
	SecretKey  string
	BaseURL    string
	PrivateKey *rsa.PrivateKey
}

// Cached B2B access token
var (
	cachedToken string
	tokenExpiry time.Time
	tokenMutex  sync.Mutex
	jakartaLoc  *time.Location
)

func init() {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		// Fallback: manually create +07:00 fixed zone
		loc = time.FixedZone("WIB", 7*60*60)
	}
	jakartaLoc = loc
}

// jakartaTimestamp returns current time formatted for Doku API in WIB (+07:00)
// Uses Google's server time to avoid local clock drift
func jakartaTimestamp() string {
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects, just need headers
		},
	}

	resp, err := client.Head("https://www.google.com")
	if err == nil {
		defer resp.Body.Close()
		if dateStr := resp.Header.Get("Date"); dateStr != "" {
			// Parse Date header: "Mon, 02 Jan 2006 15:04:05 GMT"
			if t, err := time.Parse(time.RFC1123, dateStr); err == nil {
				return t.In(jakartaLoc).Format("2006-01-02T15:04:05+07:00")
			}
		}
	}

	// Fallback to local time if HTTP check fails
	fmt.Printf("Doku Time Sync Warning: Google time check failed: %v. Using local time.\n", err)
	return time.Now().In(jakartaLoc).Format("2006-01-02T15:04:05+07:00")
}

// getDokuConfig reads Doku configuration from environment variables
func getDokuConfig() (*DokuConfig, error) {
	clientID := os.Getenv("DOKU_CLIENT_ID")
	secretKey := os.Getenv("DOKU_SECRET_KEY")
	baseURL := os.Getenv("DOKU_BASE_URL")

	// Trim whitespace from credentials
	clientID = strings.TrimSpace(clientID)
	secretKey = strings.TrimSpace(secretKey)

	if clientID == "" || secretKey == "" {
		return nil, fmt.Errorf("DOKU_CLIENT_ID and DOKU_SECRET_KEY must be set")
	}

	if baseURL == "" {
		baseURL = "https://api-sandbox.doku.com"
	} else {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}

	// Load RSA private key for asymmetric signature (token request)
	privateKeyPEM := os.Getenv("DOKU_PRIVATE_KEY")
	if privateKeyPEM == "" {
		// Try loading from file
		keyPath := os.Getenv("DOKU_PRIVATE_KEY_PATH")
		if keyPath != "" {
			keyData, err := os.ReadFile(keyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read private key file: %v", err)
			}
			privateKeyPEM = string(keyData)
		}
	}

	// Replace escaped newlines (for env var storage)
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "\\n", "\n")

	var privateKey *rsa.PrivateKey
	if privateKeyPEM != "" {
		block, _ := pem.Decode([]byte(privateKeyPEM))
		if block == nil {
			return nil, fmt.Errorf("failed to decode PEM block for private key")
		}

		// Try PKCS8 first, then PKCS1
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			key2, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err2 != nil {
				return nil, fmt.Errorf("failed to parse private key (tried PKCS8 and PKCS1): %v / %v", err, err2)
			}
			privateKey = key2
		} else {
			rsaKey, ok := key.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("private key is not RSA")
			}
			privateKey = rsaKey
		}

		// Debug: Log Public Key Modulus Prefix to verify correct key is loaded
		if privateKey != nil {
			pub := privateKey.Public().(*rsa.PublicKey)
			modulus := pub.N.Bytes()
			if len(modulus) > 10 {
				fmt.Printf("Doku Config Loaded. Key Modulus Prefix: %X...\n", modulus[:10])
			}
		}
	}

	return &DokuConfig{
		ClientID:   clientID,
		SecretKey:  secretKey,
		BaseURL:    baseURL,
		PrivateKey: privateKey,
	}, nil
}

// generateAsymmetricSignature creates RSA-SHA256 signature for B2B token request
// StringToSign = ClientID + "|" + Timestamp
func generateAsymmetricSignature(privateKey *rsa.PrivateKey, clientID, timestamp string) (string, error) {
	if privateKey == nil {
		return "", fmt.Errorf("RSA private key not configured")
	}

	stringToSign := clientID + "|" + timestamp

	// Log StringToSign for debugging
	fmt.Printf("Doku Asymmetric Signature Debug:\nString: %s\n", stringToSign)

	hash := sha256.Sum256([]byte(stringToSign))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign: %v", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

// generateSymmetricSignature creates HMAC-SHA512 signature for transactional API calls
// StringToSign = HTTPMethod + ":" + EndpointUrl + ":" + AccessToken + ":" + Lowercase(HexEncode(SHA-256(minify(RequestBody)))) + ":" + TimeStamp
func generateSymmetricSignature(secretKey, httpMethod, endpointURL, accessToken, requestBody, timestamp string) string {
	// SHA-256 hash of minified request body
	bodyHash := sha256.Sum256([]byte(requestBody))
	bodyHashHex := strings.ToLower(hex.EncodeToString(bodyHash[:]))

	stringToSign := httpMethod + ":" + endpointURL + ":" + accessToken + ":" + bodyHashHex + ":" + timestamp

	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write([]byte(stringToSign))
	signature := mac.Sum(nil)
	encodedSignature := base64.StdEncoding.EncodeToString(signature)

	// Log signature components for debugging
	fmt.Printf("Doku Signature Debug:\nString: %s\nSig: %s\n", stringToSign, encodedSignature)

	return encodedSignature
}

// getB2BAccessToken obtains or returns cached Doku B2B access token
func getB2BAccessToken(config *DokuConfig) (string, error) {
	tokenMutex.Lock()
	defer tokenMutex.Unlock()

	// Return cached token if still valid (with 60s buffer)
	if cachedToken != "" && time.Now().Before(tokenExpiry.Add(-60*time.Second)) {
		return cachedToken, nil
	}

	timestamp := jakartaTimestamp()

	// Generate asymmetric signature
	signature, err := generateAsymmetricSignature(config.PrivateKey, config.ClientID, timestamp)
	if err != nil {
		return "", fmt.Errorf("failed to generate signature: %v", err)
	}

	// Build request
	reqBody := map[string]string{
		"grantType": "client_credentials",
	}
	reqJSON, _ := json.Marshal(reqBody)

	url := config.BaseURL + "/authorization/v1/access-token/b2b"
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-CLIENT-KEY", config.ClientID)
	httpReq.Header.Set("X-TIMESTAMP", timestamp)
	httpReq.Header.Set("X-SIGNATURE", signature)

	// Debug headers
	fmt.Printf("Doku Token Request Headers:\nX-CLIENT-KEY: %s\nX-TIMESTAMP: %s\nX-SIGNATURE: %s\n", config.ClientID, timestamp, signature)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Doku: %v", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		ResponseCode    string `json:"responseCode"`
		ResponseMessage string `json:"responseMessage"`
		AccessToken     string `json:"accessToken"`
		TokenType       string `json:"tokenType"`
		ExpiresIn       int    `json:"expiresIn"` // seconds
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %v", err)
	}

	if resp.StatusCode != 200 || tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token request failed: %s - %s", tokenResp.ResponseCode, tokenResp.ResponseMessage)
	}

	// Cache the token
	cachedToken = tokenResp.AccessToken
	tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	fmt.Printf("Doku B2B access token obtained, expires in %d seconds\n", tokenResp.ExpiresIn)
	return cachedToken, nil
}

// DokuQRISRequest is the request body for generating QRIS
type DokuQRISRequest struct {
	PartnerReferenceNo string          `json:"partnerReferenceNo"`
	Amount             DokuAmount      `json:"amount"`
	MerchantID         string          `json:"merchantId"`
	TerminalID         string          `json:"terminalId,omitempty"`
	ValidityPeriod     string          `json:"validityPeriod,omitempty"` // ISO 8601 duration, e.g. "PT30M"
	AdditionalInfo     *DokuAdditional `json:"additionalInfo,omitempty"`
}

type DokuAmount struct {
	Value    string `json:"value"`    // e.g. "54390.00"
	Currency string `json:"currency"` // "IDR"
}

type DokuAdditional struct {
	Description string `json:"description,omitempty"`
}

// DokuQRISResponse is the response from QRIS generation
type DokuQRISResponse struct {
	ResponseCode       string `json:"responseCode"`
	ResponseMessage    string `json:"responseMessage"`
	ReferenceNo        string `json:"referenceNo"`
	PartnerReferenceNo string `json:"partnerReferenceNo"`
	QRContent          string `json:"qrContent"`
	QRUrl              string `json:"qrUrl,omitempty"`
	TerminalID         string `json:"terminalId,omitempty"`
}

// generateQRIS calls Doku API to generate a QRIS code
func generateQRIS(config *DokuConfig, accessToken string, req DokuQRISRequest) (*DokuQRISResponse, error) {
	endpointPath := "/snap/v1.0/qr/qr-mpm-generate"
	url := config.BaseURL + endpointPath

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
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
		return nil, fmt.Errorf("failed to create request: %v", err)
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
		return nil, fmt.Errorf("failed to connect to Doku QRIS API: %v", err)
	}
	defer resp.Body.Close()

	var qrisResp DokuQRISResponse
	if err := json.NewDecoder(resp.Body).Decode(&qrisResp); err != nil {
		return nil, fmt.Errorf("failed to decode QRIS response: %v", err)
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("QRIS generation failed [%d]: %s - %s", resp.StatusCode, qrisResp.ResponseCode, qrisResp.ResponseMessage)
	}

	return &qrisResp, nil
}

// DokuQueryRequest is the request body for querying QRIS status
type DokuQueryRequest struct {
	OriginalPartnerReferenceNo string     `json:"originalPartnerReferenceNo"`
	OriginalReferenceNo        string     `json:"originalReferenceNo,omitempty"`
	ServiceCode                string     `json:"serviceCode"`
	Amount                     DokuAmount `json:"amount,omitempty"`
}

// DokuQueryResponse is the response from QRIS status query
type DokuQueryResponse struct {
	ResponseCode             string `json:"responseCode"`
	ResponseMessage          string `json:"responseMessage"`
	OriginalReferenceNo      string `json:"originalReferenceNo"`
	LatestTransactionStatus  string `json:"latestTransactionStatus"` // 00=success, 06=pending
	TransactionStatusDesc    string `json:"transactionStatusDesc"`
	PaidTime                 string `json:"paidTime,omitempty"`
}

// queryQRISStatus calls Doku API to check QRIS payment status
func queryQRISStatus(config *DokuConfig, accessToken string, req DokuQueryRequest) (*DokuQueryResponse, error) {
	endpointPath := "/snap/v1.0/qr/qr-mpm-query"
	url := config.BaseURL + endpointPath

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
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
		return nil, fmt.Errorf("failed to create request: %v", err)
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
		return nil, fmt.Errorf("failed to connect to Doku Query API: %v", err)
	}
	defer resp.Body.Close()

	var queryResp DokuQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		return nil, fmt.Errorf("failed to decode query response: %v", err)
	}

	return &queryResp, nil
}
