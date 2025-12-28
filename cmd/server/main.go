package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/yuditriaji/warungin-backend/internal/auth"
	"github.com/yuditriaji/warungin-backend/internal/dashboard"
	"github.com/yuditriaji/warungin-backend/internal/product"
	"github.com/yuditriaji/warungin-backend/internal/reports"
	"github.com/yuditriaji/warungin-backend/internal/transaction"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"github.com/yuditriaji/warungin-backend/pkg/middleware"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Initialize database
	db, err := database.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Run migrations
	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Setup Gin router
	r := gin.Default()

	// Middleware
	r.Use(middleware.CORS())

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API v1 routes
	v1 := r.Group("/api/v1")
	{
		// Auth routes (public)
		authHandler := auth.NewHandler(db)
		v1.POST("/auth/register", authHandler.Register)
		v1.POST("/auth/login", authHandler.Login)
		v1.POST("/auth/refresh", authHandler.RefreshToken)
		
		// Google OAuth routes
		v1.GET("/auth/google", authHandler.GoogleLogin)
		v1.GET("/auth/google/callback", authHandler.GoogleCallback)

		// Protected routes
		protected := v1.Group("")
		protected.Use(middleware.AuthRequired())
		{
			// Auth - get current user
			protected.GET("/auth/me", authHandler.GetMe)
			
			// Dashboard routes
			dashboardHandler := dashboard.NewHandler(db)
			protected.GET("/dashboard/stats", dashboardHandler.GetStats)
			protected.GET("/dashboard/top-products", dashboardHandler.GetTopProducts)
			protected.GET("/dashboard/recent-transactions", dashboardHandler.GetRecentTransactions)
			
			// Product routes
			productHandler := product.NewHandler(db)
			protected.GET("/products", productHandler.List)
			protected.POST("/products", productHandler.Create)
			protected.GET("/products/:id", productHandler.Get)
			protected.PUT("/products/:id", productHandler.Update)
			protected.DELETE("/products/:id", productHandler.Delete)

			// Transaction routes
			transactionHandler := transaction.NewHandler(db)
			protected.GET("/transactions", transactionHandler.List)
			protected.POST("/transactions", transactionHandler.Create)
			protected.GET("/transactions/:id", transactionHandler.Get)

			// Reports routes
			reportsHandler := reports.NewHandler(db)
			protected.GET("/reports/sales", reportsHandler.GetSalesReport)
			protected.GET("/reports/products", reportsHandler.GetProductSalesReport)
		}
	}

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
