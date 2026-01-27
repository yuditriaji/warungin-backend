package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/yuditriaji/warungin-backend/internal/auth"
	"github.com/yuditriaji/warungin-backend/internal/customer"
	"github.com/yuditriaji/warungin-backend/internal/dashboard"
	"github.com/yuditriaji/warungin-backend/internal/inventory"
	"github.com/yuditriaji/warungin-backend/internal/material"
	"github.com/yuditriaji/warungin-backend/internal/outlet"
	"github.com/yuditriaji/warungin-backend/internal/payment"
	"github.com/yuditriaji/warungin-backend/internal/product"
	"github.com/yuditriaji/warungin-backend/internal/reports"
	"github.com/yuditriaji/warungin-backend/internal/subscription"
	"github.com/yuditriaji/warungin-backend/internal/tenant"
	"github.com/yuditriaji/warungin-backend/internal/transaction"
	"github.com/yuditriaji/warungin-backend/internal/user"
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

			// Limit checker
			limitChecker := middleware.NewLimitChecker(db)
			
			// Product routes (with limit check)
			productHandler := product.NewHandler(db)
			protected.GET("/products", productHandler.List)
			protected.POST("/products", limitChecker.CheckProductLimit(), productHandler.Create)
			protected.GET("/products/:id", productHandler.Get)
			protected.PUT("/products/:id", productHandler.Update)
			protected.DELETE("/products/:id", productHandler.Delete)
			protected.PATCH("/products/:id/toggle", productHandler.ToggleActive)

			// Transaction routes (with limit check)
			transactionHandler := transaction.NewHandler(db)
			protected.GET("/transactions", transactionHandler.List)
			protected.POST("/transactions", limitChecker.CheckTransactionLimit(), transactionHandler.Create)
		protected.GET("/transactions/:id", transactionHandler.Get)
			protected.POST("/transactions/:id/void", transactionHandler.Void)
			protected.GET("/audit-logs", transactionHandler.ListAuditLogs)

			// Reports routes
			reportsHandler := reports.NewHandler(db)
			protected.GET("/reports/sales", reportsHandler.GetSalesReport)
			protected.GET("/reports/products", reportsHandler.GetProductSalesReport)

			// Customer routes
			customerHandler := customer.NewHandler(db)
			protected.GET("/customers", customerHandler.List)
			protected.POST("/customers", customerHandler.Create)
			protected.GET("/customers/:id", customerHandler.Get)
			protected.PUT("/customers/:id", customerHandler.Update)
			protected.DELETE("/customers/:id", customerHandler.Delete)
			protected.GET("/customers/:id/stats", customerHandler.GetStats)

			// Inventory routes
			inventoryHandler := inventory.NewHandler(db)
			protected.GET("/inventory", inventoryHandler.GetInventory)
			protected.GET("/inventory/summary", inventoryHandler.GetSummary)
			protected.GET("/inventory/alerts", inventoryHandler.GetAlerts)
			protected.PUT("/inventory/:id/stock", inventoryHandler.UpdateStock)

			// Payment routes
			paymentHandler := payment.NewHandler(db)
			protected.POST("/payment/qris", paymentHandler.CreateQRIS)
			protected.GET("/payment/status/:order_id", paymentHandler.CheckStatus)

			// Subscription routes
			subscriptionHandler := subscription.NewHandler(db)
			protected.GET("/subscription/plans", subscriptionHandler.GetPlans)
			protected.GET("/subscription", subscriptionHandler.GetCurrent)
			protected.GET("/subscription/usage", subscriptionHandler.GetUsage)
			protected.POST("/subscription/upgrade", subscriptionHandler.Upgrade)

			// Payment routes (Xendit)
			paymentH := payment.NewHandler(db)
			protected.POST("/payment/invoice", paymentH.CreateSubscriptionInvoice)
			protected.GET("/payment/invoice/:invoice_id/status", paymentH.GetInvoiceStatus)

			// Tenant settings routes
			tenantHandler := tenant.NewHandler(db)
			protected.GET("/tenant/settings", tenantHandler.GetSettings)
			protected.PUT("/tenant/settings", tenantHandler.UpdateSettings)
			protected.POST("/tenant/qris-upload", tenantHandler.UploadQRIS)
			protected.PUT("/tenant/profile", tenantHandler.UpdateProfile)

			// Material routes
			materialHandler := material.NewHandler(db)
			protected.GET("/materials", materialHandler.List)
			protected.POST("/materials", materialHandler.Create)
			protected.GET("/materials/:id", materialHandler.Get)
			protected.PUT("/materials/:id", materialHandler.Update)
			protected.DELETE("/materials/:id", materialHandler.Delete)
			protected.PUT("/materials/:id/stock", materialHandler.UpdateStock)
			protected.GET("/materials/alerts", materialHandler.GetAlerts)

			// Product-Material linkage (using separate path to avoid conflict with /products/:id)
			protected.GET("/product-materials/:product_id", materialHandler.GetProductMaterials)
			protected.POST("/product-materials", materialHandler.LinkMaterial)
			protected.DELETE("/product-materials/:product_id/:material_id", materialHandler.UnlinkMaterial)
			protected.GET("/product-materials/:product_id/cost", materialHandler.CalculateProductCost)

			// Outlet routes
			outletHandler := outlet.NewHandler(db)
			protected.GET("/outlets", outletHandler.List)
			protected.POST("/outlets", outletHandler.Create)
			protected.GET("/outlets/:id", outletHandler.Get)
			protected.PUT("/outlets/:id", outletHandler.Update)
			protected.DELETE("/outlets/:id", outletHandler.Delete)
			protected.GET("/outlets/:id/stats", outletHandler.GetStats)
			protected.POST("/outlets/:id/switch", outletHandler.SwitchOutlet)

			// Staff routes
			userHandler := user.NewHandler(db)
			protected.GET("/staff", userHandler.ListStaff)
			protected.POST("/staff", userHandler.CreateStaff)
			protected.PUT("/staff/:id", userHandler.UpdateStaff)
			protected.DELETE("/staff/:id", userHandler.DeleteStaff)
			protected.GET("/staff/logs", userHandler.GetActivityLogs)
		}

		// Webhooks (public, no auth)
		paymentHandler := payment.NewHandler(db)
		v1.POST("/webhook/xendit", paymentHandler.XenditWebhook)
		v1.GET("/webhook/xendit", paymentHandler.WebhookVerify)  // For URL verification
		v1.POST("/webhook/midtrans", paymentHandler.Webhook) // Legacy support
		v1.GET("/webhook/midtrans", paymentHandler.WebhookVerify)
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
