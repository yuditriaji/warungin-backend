package portal

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// PortalAuthMiddleware validates portal JWT tokens
func PortalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Bearer token required"})
			c.Abort()
			return
		}

		jwtSecret := os.Getenv("JWT_SECRET")
		if jwtSecret == "" {
			jwtSecret = "default-secret-change-in-production"
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		// Set user info in context
		c.Set("portal_user_id", claims["portal_user_id"])
		c.Set("portal_email", claims["email"])
		c.Set("portal_role", claims["role"])

		c.Next()
	}
}

// SuperAdminMiddleware ensures user is super_admin
func SuperAdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString("portal_role")
		if role != "super_admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Super Admin access required"})
			c.Abort()
			return
		}
		c.Next()
	}
}
