package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TenantSettings represents configurable settings for a tenant
type TenantSettings struct {
	QRISEnabled  bool    `json:"qris_enabled"`   // Whether QRIS payment is enabled
	QRISImageURL string  `json:"qris_image_url"` // URL to merchant's static QRIS image
	QRISLabel    string  `json:"qris_label"`     // Display name, e.g., "BCA QRIS"
	TaxEnabled   bool    `json:"tax_enabled"`    // Whether global tax/PPN is enabled
	TaxRate      float64 `json:"tax_rate"`       // Tax percentage (e.g., 11 for PPN 11%)
	TaxLabel     string  `json:"tax_label"`      // Label for tax, e.g., "PPN 11%"
}

// Base model for all entities
type BaseModel struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// Tenant represents a business/organization
type Tenant struct {
	BaseModel
	Name         string        `gorm:"not null" json:"name"`
	BusinessType string        `json:"business_type"`
	Phone        string        `json:"phone"`
	Email        string        `gorm:"uniqueIndex" json:"email"`
	Address      string        `json:"address"`         // Legacy/full address text
	ProvinceID   string        `json:"province_id"`     // External API ID
	ProvinceName string        `json:"province_name"`
	CityID       string        `json:"city_id"`         // External API ID
	CityName     string        `json:"city_name"`
	PostalCode   string        `json:"postal_code"`
	Settings     string        `gorm:"type:jsonb;default:'{}'" json:"settings"`
	Subscription *Subscription `gorm:"foreignKey:TenantID" json:"subscription,omitempty"`
	Outlets      []Outlet      `gorm:"foreignKey:TenantID" json:"outlets,omitempty"`
}

// Subscription represents tenant's plan
type Subscription struct {
	BaseModel
	TenantID               uuid.UUID  `gorm:"type:uuid;uniqueIndex;not null" json:"tenant_id"`
	Plan                   string     `gorm:"default:'gratis'" json:"plan"` // gratis, pemula, bisnis, enterprise
	Status                 string     `gorm:"default:'active'" json:"status"` // active, past_due, cancelled
	MaxUsers               int        `gorm:"default:1" json:"max_users"`
	MaxProducts            int        `gorm:"default:20" json:"max_products"`
	MaxTransactionsDaily   int        `gorm:"default:20" json:"max_transactions_daily"` // For gratis tier
	MaxTransactionsMonthly int        `gorm:"default:0" json:"max_transactions_monthly"` // 0 = unlimited
	MaxOutlets             int        `gorm:"default:1" json:"max_outlets"`
	DataRetentionDays      int        `gorm:"default:30" json:"data_retention_days"`
	CurrentPeriodStart     time.Time  `json:"current_period_start"`
	CurrentPeriodEnd       time.Time  `json:"current_period_end"`
	PaymentGatewayID       string     `json:"-"` // Hidden - Midtrans subscription ID
}

// UsageMetrics tracks tenant usage per period
type UsageMetrics struct {
	BaseModel
	TenantID         uuid.UUID `gorm:"type:uuid;not null;index" json:"tenant_id"`
	Period           string    `gorm:"not null" json:"period"` // YYYY-MM format
	TransactionCount int       `gorm:"default:0" json:"transaction_count"`
	ProductCount     int       `gorm:"default:0" json:"product_count"`
	UserCount        int       `gorm:"default:0" json:"user_count"`
	DailyTxDate      string    `json:"daily_tx_date"` // YYYY-MM-DD for daily tracking
	DailyTxCount     int       `gorm:"default:0" json:"daily_tx_count"`
}

// Outlet represents a store location
type Outlet struct {
	BaseModel
	TenantID     uuid.UUID `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant       Tenant    `gorm:"foreignKey:TenantID" json:"-"`
	Name         string    `gorm:"not null" json:"name"`
	BusinessType string    `json:"business_type"` // fnb, retail, barbershop, etc.
	Address      string    `json:"address"`       // Street address detail
	ProvinceID   string    `json:"province_id"`
	ProvinceName string    `json:"province_name"`
	CityID       string    `json:"city_id"`
	CityName     string    `json:"city_name"`
	PostalCode   string    `json:"postal_code"`
	Phone        string    `json:"phone"`
	IsActive     bool      `gorm:"default:true" json:"is_active"`
}

// User represents a system user
type User struct {
	BaseModel
	TenantID     uuid.UUID  `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant       Tenant     `gorm:"foreignKey:TenantID" json:"-"`
	OutletID     *uuid.UUID `gorm:"type:uuid" json:"outlet_id"` // Optional outlet assignment
	Outlet       *Outlet    `gorm:"foreignKey:OutletID" json:"outlet,omitempty"`
	Email        string     `gorm:"uniqueIndex;not null" json:"email"`
	GoogleID     string     `gorm:"index" json:"-"` // For Google OAuth (not unique to allow null for staff)
	PasswordHash string     `json:"-"`                     // Optional for OAuth users
	Name         string     `gorm:"not null" json:"name"`
	Role         string     `gorm:"default:'cashier'" json:"role"` // owner, manager, cashier
	IsActive     bool       `gorm:"default:true" json:"is_active"`
}

// Category for products
type Category struct {
	BaseModel
	TenantID uuid.UUID `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant   Tenant    `gorm:"foreignKey:TenantID" json:"-"`
	Name     string    `gorm:"not null" json:"name"`
}

// Product represents a sellable item
type Product struct {
	BaseModel
	TenantID         uuid.UUID  `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant           Tenant     `gorm:"foreignKey:TenantID" json:"-"`
	OutletID         *uuid.UUID `gorm:"type:uuid" json:"outlet_id"`
	Outlet           *Outlet    `gorm:"foreignKey:OutletID" json:"outlet,omitempty"`
	CategoryID       *uuid.UUID `gorm:"type:uuid" json:"category_id"`
	Category         *Category  `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Name             string     `gorm:"not null" json:"name"`
	SKU              string     `json:"sku"`
	Price            float64    `gorm:"not null" json:"price"`
	Cost             float64    `json:"cost"`
	TaxRate          float64    `gorm:"default:0" json:"tax_rate"` // Tax percentage (e.g., 10 for 10%)
	StockQty         int        `gorm:"default:0" json:"stock_qty"`
	UseMaterialStock bool       `gorm:"default:false" json:"use_material_stock"` // When true, stock is calculated from linked materials
	ImageURL         string     `json:"image_url"`
	IsActive         bool       `gorm:"default:true" json:"is_active"`
	Modifiers        []ProductModifier `gorm:"foreignKey:ProductID" json:"modifiers,omitempty"`
}

// ProductModifier represents add-ons/variations (e.g., sizes, toppings)
type ProductModifier struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	ProductID uuid.UUID `gorm:"type:uuid;not null" json:"product_id"`
	Name      string    `gorm:"not null" json:"name"` // e.g., "Size", "Topping"
	Options   string    `gorm:"type:text" json:"options"` // JSON array: [{"name":"Large", "price":5000}]
	IsRequired bool     `gorm:"default:false" json:"is_required"`
}

// RawMaterial represents raw materials/ingredients
type RawMaterial struct {
	BaseModel
	TenantID      uuid.UUID  `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant        Tenant     `gorm:"foreignKey:TenantID" json:"-"`
	OutletID      *uuid.UUID `gorm:"type:uuid" json:"outlet_id"` // Optional outlet assignment
	Outlet        *Outlet    `gorm:"foreignKey:OutletID" json:"outlet,omitempty"`
	Name          string     `gorm:"not null" json:"name"`
	Unit          string     `gorm:"not null" json:"unit"` // kg, liter, pcs, etc.
	UnitPrice     float64    `json:"unit_price"`
	StockQty      float64    `gorm:"default:0" json:"stock_qty"`
	MinStockLevel float64    `gorm:"default:10" json:"min_stock_level"` // Alert threshold
	Supplier      string     `json:"supplier"`
}

// ProductMaterial links products to raw materials
type ProductMaterial struct {
	ID             uuid.UUID   `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	ProductID      uuid.UUID   `gorm:"type:uuid;not null" json:"product_id"`
	Product        Product     `gorm:"foreignKey:ProductID" json:"-"`
	MaterialID     uuid.UUID   `gorm:"type:uuid;not null" json:"material_id"`
	Material       RawMaterial `gorm:"foreignKey:MaterialID" json:"material"`
	QuantityUsed   float64     `gorm:"not null" json:"quantity_used"`
	UsedUnit       string      `json:"used_unit"`       // Unit used in recipe (can differ from material unit)
	ConversionRate float64     `gorm:"default:1" json:"conversion_rate"` // Multiply to convert to material unit
}

// Customer represents a buyer
type Customer struct {
	BaseModel
	TenantID uuid.UUID `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant   Tenant    `gorm:"foreignKey:TenantID" json:"-"`
	Name     string    `gorm:"not null" json:"name"`
	Phone    string    `json:"phone"`
	Email    string    `json:"email"`
	Address  string    `json:"address"`
}

// Transaction represents a sale
type Transaction struct {
	BaseModel
	TenantID      uuid.UUID         `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant        Tenant            `gorm:"foreignKey:TenantID" json:"-"`
	OutletID      *uuid.UUID        `gorm:"type:uuid" json:"outlet_id"`
	Outlet        *Outlet           `gorm:"foreignKey:OutletID" json:"outlet,omitempty"`
	InvoiceNumber string            `gorm:"uniqueIndex;not null" json:"invoice_number"`
	OrderNumber   int               `gorm:"default:0" json:"order_number"` // Queue number, resets daily
	UserID        uuid.UUID         `gorm:"type:uuid;not null" json:"user_id"`
	User          User              `gorm:"foreignKey:UserID" json:"user,omitempty"`
	CustomerID    *uuid.UUID        `gorm:"type:uuid" json:"customer_id"`
	Customer      *Customer         `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Items         []TransactionItem `gorm:"foreignKey:TransactionID" json:"items"`
	Subtotal      float64           `gorm:"not null" json:"subtotal"`
	Discount      float64           `gorm:"default:0" json:"discount"`
	Tax           float64           `gorm:"default:0" json:"tax"`
	Total         float64           `gorm:"not null" json:"total"`
	Status        string            `gorm:"default:'completed'" json:"status"` // completed, voided, pending
	PaymentMethod string            `gorm:"default:'cash'" json:"payment_method"` // cash, qris, gopay, ovo, dana
	PaymentRef    string            `json:"payment_ref"` // Midtrans transaction ID
	IsSynced      bool              `gorm:"default:true" json:"is_synced"` // For offline support
}

// TransactionItem represents items in a transaction
type TransactionItem struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TransactionID uuid.UUID `gorm:"type:uuid;not null" json:"transaction_id"`
	ProductID     uuid.UUID `gorm:"type:uuid;not null" json:"product_id"`
	Product       Product   `gorm:"foreignKey:ProductID" json:"product"`
	Quantity      int       `gorm:"not null" json:"quantity"`
	UnitPrice     float64   `gorm:"not null" json:"unit_price"`
	Subtotal      float64   `gorm:"not null" json:"subtotal"`
}

// Invoice represents subscription billing invoices
type Invoice struct {
	BaseModel
	SubscriptionID uuid.UUID `gorm:"type:uuid;not null" json:"subscription_id"`
	InvoiceNumber  string    `gorm:"uniqueIndex;not null" json:"invoice_number"`
	Amount         float64   `gorm:"not null" json:"amount"`
	Status         string    `gorm:"default:'pending'" json:"status"` // pending, paid, failed
	DueDate        time.Time `json:"due_date"`
	PaidAt         *time.Time `json:"paid_at"`
	PaymentRef     string    `json:"payment_ref"` // Midtrans payment ID
}

// EmployeeInvite represents pending employee invitations
type EmployeeInvite struct {
	BaseModel
	TenantID     uuid.UUID  `gorm:"type:uuid;not null" json:"tenant_id"`
	InvitedBy    uuid.UUID  `gorm:"type:uuid;not null" json:"invited_by"` // Manager or Owner
	Email        string     `gorm:"not null" json:"email"`
	Role         string     `gorm:"default:'cashier'" json:"role"`
	Status       string     `gorm:"default:'pending'" json:"status"` // pending, approved, rejected, accepted
	ApprovedBy   *uuid.UUID `gorm:"type:uuid" json:"approved_by"` // Owner who approved
	Token        string     `gorm:"uniqueIndex" json:"-"` // Invite token
	ExpiresAt    time.Time  `json:"expires_at"`
}

// ActivityLog tracks staff actions for audit trail
type ActivityLog struct {
	ID         uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TenantID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"tenant_id"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null" json:"user_id"`
	User       User       `gorm:"foreignKey:UserID" json:"user"`
	OutletID   *uuid.UUID `gorm:"type:uuid" json:"outlet_id"`
	Action     string     `gorm:"not null" json:"action"` // login, logout, sale, void, stock_adjust, etc.
	EntityType string     `json:"entity_type"` // transaction, product, material, etc.
	EntityID   *uuid.UUID `gorm:"type:uuid" json:"entity_id"`
	Details    string     `gorm:"type:text" json:"details"` // JSON details
	IPAddress  string     `json:"ip_address"`
	CreatedAt  time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TransactionAuditLog tracks transaction changes with detail snapshots
type TransactionAuditLog struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TenantID      uuid.UUID  `gorm:"type:uuid;not null;index" json:"tenant_id"`
	TransactionID uuid.UUID  `gorm:"type:uuid;not null;index" json:"transaction_id"`
	Transaction   Transaction `gorm:"foreignKey:TransactionID" json:"transaction,omitempty"`
	Action        string     `gorm:"not null" json:"action"` // void, correction, refund
	Reason        string     `gorm:"not null" json:"reason"` // Required justification
	OldValues     string     `gorm:"type:jsonb" json:"old_values"` // Snapshot of old data
	NewValues     string     `gorm:"type:jsonb" json:"new_values"` // Changes made (if any)
	UserID        uuid.UUID  `gorm:"type:uuid;not null" json:"user_id"`
	User          User       `gorm:"foreignKey:UserID" json:"user,omitempty"`
	ManagerID     *uuid.UUID `gorm:"type:uuid" json:"manager_id"` // If manager approval was required
	Manager       *User      `gorm:"foreignKey:ManagerID" json:"manager,omitempty"`
	IPAddress     string     `json:"ip_address"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// Migrate runs database migrations
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Tenant{},
		&Subscription{},
		&UsageMetrics{},
		&Outlet{},
		&User{},
		&Category{},
		&Product{},
		&ProductModifier{},
		&RawMaterial{},
		&ProductMaterial{},
		&Customer{},
		&Transaction{},
		&TransactionItem{},
		&Invoice{},
		&EmployeeInvite{},
		&ActivityLog{},
		&TransactionAuditLog{},
	)
}

