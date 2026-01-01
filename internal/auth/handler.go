package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/gorm"
)

type Handler struct {
	db           *gorm.DB
	googleConfig *oauth2.Config
}

func NewHandler(db *gorm.DB) *Handler {
	googleConfig := &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint:     google.Endpoint,
	}

	return &Handler{
		db:           db,
		googleConfig: googleConfig,
	}
}

type RegisterRequest struct {
	BusinessName string `json:"business_name" binding:"required"`
	BusinessType string `json:"business_type"`
	Email        string `json:"email" binding:"required,email"`
	Password     string `json:"password" binding:"required,min=6"`
	Name         string `json:"name" binding:"required"`
	Phone        string `json:"phone"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	AccessToken  string              `json:"access_token"`
	RefreshToken string              `json:"refresh_token"`
	ExpiresIn    int64               `json:"expires_in"`
	User         database.User       `json:"user"`
	Tenant       database.Tenant     `json:"tenant"`
	IsNewUser    bool                `json:"is_new_user,omitempty"`
}

type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
}

// GoogleLogin redirects to Google OAuth consent screen
func (h *Handler) GoogleLogin(c *gin.Context) {
	// Generate state token for CSRF protection
	state := uuid.New().String()
	
	// Store state in cookie (short-lived)
	c.SetCookie("oauth_state", state, 300, "/", "", false, true)
	
	url := h.googleConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// GoogleCallback handles the OAuth callback from Google
func (h *Handler) GoogleCallback(c *gin.Context) {
	// Verify state
	state := c.Query("state")
	storedState, err := c.Cookie("oauth_state")
	if err != nil || state != storedState {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state parameter"})
		return
	}

	// Get authorization code
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No authorization code"})
		return
	}

	// Exchange code for token
	token, err := h.googleConfig.Exchange(context.Background(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to exchange token"})
		return
	}

	// Get user info from Google
	userInfo, err := h.getGoogleUserInfo(token.AccessToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user info"})
		return
	}

	// Check if user exists
	var user database.User
	var tenant database.Tenant
	isNewUser := false

	err = h.db.Where("google_id = ?", userInfo.ID).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		// Try to find by email
		err = h.db.Where("email = ?", userInfo.Email).First(&user).Error
		if err == gorm.ErrRecordNotFound {
			// New user - need to create tenant and user
			isNewUser = true
			
			// Create tenant
			tenant = database.Tenant{
				Name:  userInfo.Name + "'s Business",
				Email: userInfo.Email,
			}
			if err := h.db.Create(&tenant).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create business"})
				return
			}

			// Create default subscription (Gratis)
			subscription := database.Subscription{
				TenantID:               tenant.ID,
				Plan:                   "gratis",
				Status:                 "active",
				MaxUsers:               1,
				MaxProducts:            20,
				MaxTransactionsDaily:   20,
				MaxTransactionsMonthly: 0, // 0 = use daily limit
				MaxOutlets:             1,
				DataRetentionDays:      30,
				CurrentPeriodStart:     time.Now(),
				CurrentPeriodEnd:       time.Now().AddDate(0, 1, 0),
			}
			h.db.Create(&subscription)

			// Create user
			user = database.User{
				TenantID: tenant.ID,
				Email:    userInfo.Email,
				GoogleID: userInfo.ID,
				Name:     userInfo.Name,
				Role:     "owner",
				IsActive: true,
			}
			if err := h.db.Create(&user).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
				return
			}
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		} else {
			// User exists by email, update GoogleID
			user.GoogleID = userInfo.ID
			h.db.Save(&user)
			h.db.First(&tenant, user.TenantID)
		}
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	} else {
		// User found by GoogleID
		h.db.First(&tenant, user.TenantID)
	}

	// Generate tokens
	accessToken, refreshToken, _ := generateTokens(user, tenant)

	// Get frontend URL for redirect
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	// Pass tokens via URL query parameters (required for cross-domain OAuth flow)
	// Frontend will immediately store these in localStorage and the URL will be replaced
	redirectURL := fmt.Sprintf("%s/auth/callback?access_token=%s&refresh_token=%s&is_new_user=%t",
		frontendURL, accessToken, refreshToken, isNewUser)
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

func (h *Handler) getGoogleUserInfo(accessToken string) (*GoogleUserInfo, error) {
	resp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + accessToken)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var userInfo GoogleUserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// Register creates a new tenant and owner user (email/password)
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if email exists
	var existingUser database.User
	if err := h.db.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Create tenant
	tenant := database.Tenant{
		Name:         req.BusinessName,
		BusinessType: req.BusinessType,
		Phone:        req.Phone,
		Email:        req.Email,
	}

	if err := h.db.Create(&tenant).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create business"})
		return
	}

	// Create default subscription (Gratis)
	subscription := database.Subscription{
		TenantID:               tenant.ID,
		Plan:                   "gratis",
		Status:                 "active",
		MaxUsers:               1,
		MaxProducts:            20,
		MaxTransactionsDaily:   20,
		MaxTransactionsMonthly: 0,
		MaxOutlets:             1,
		DataRetentionDays:      30,
		CurrentPeriodStart:     time.Now(),
		CurrentPeriodEnd:       time.Now().AddDate(0, 1, 0),
	}
	h.db.Create(&subscription)

	// Create owner user
	user := database.User{
		TenantID:     tenant.ID,
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Role:         "owner",
		IsActive:     true,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Generate tokens
	accessToken, refreshToken, expiresIn := generateTokens(user, tenant)

	c.JSON(http.StatusCreated, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		User:         user,
		Tenant:       tenant,
	})
}

// Login authenticates a user with email/password
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find user
	var user database.User
	if err := h.db.Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Get tenant
	var tenant database.Tenant
	if err := h.db.First(&tenant, user.TenantID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get business info"})
		return
	}

	// Generate tokens
	accessToken, refreshToken, expiresIn := generateTokens(user, tenant)

	c.JSON(http.StatusOK, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		User:         user,
		Tenant:       tenant,
	})
}

// RefreshToken generates new tokens from a refresh token
func (h *Handler) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "your-secret-key-change-in-production"
	}

	token, err := jwt.Parse(req.RefreshToken, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
		return
	}

	userIDStr, _ := claims["user_id"].(string)
	userID, _ := uuid.Parse(userIDStr)

	var user database.User
	if err := h.db.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	var tenant database.Tenant
	if err := h.db.First(&tenant, user.TenantID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get business info"})
		return
	}

	accessToken, refreshToken, expiresIn := generateTokens(user, tenant)

	c.JSON(http.StatusOK, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		User:         user,
		Tenant:       tenant,
	})
}

// GetMe returns the current user's info
func (h *Handler) GetMe(c *gin.Context) {
	userID, _ := c.Get("user_id")
	tenantID, _ := c.Get("tenant_id")

	var user database.User
	if err := h.db.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var tenant database.Tenant
	if err := h.db.Preload("Subscription").Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Business not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":   user,
		"tenant": tenant,
	})
}

func generateTokens(user database.User, tenant database.Tenant) (string, string, int64) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "your-secret-key-change-in-production"
	}

	expiresIn := int64(15 * 60) // 15 minutes

	// Access token
	accessClaims := jwt.MapClaims{
		"user_id":   user.ID.String(),
		"tenant_id": tenant.ID.String(),
		"email":     user.Email,
		"role":      user.Role,
		"exp":       time.Now().Add(15 * time.Minute).Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, _ := accessToken.SignedString([]byte(secret))

	// Refresh token (7 days)
	refreshClaims := jwt.MapClaims{
		"user_id": user.ID.String(),
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, _ := refreshToken.SignedString([]byte(secret))

	return accessTokenString, refreshTokenString, expiresIn
}
