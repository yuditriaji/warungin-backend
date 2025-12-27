package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
	Name         string `gorm:"not null" json:"name"`
	BusinessType string `json:"business_type"`
	Phone        string `json:"phone"`
	Email        string `gorm:"uniqueIndex" json:"email"`
	Address      string `json:"address"`
	Settings     string `gorm:"type:jsonb;default:'{}'" json:"settings"`
}

// User represents a system user
type User struct {
	BaseModel
	TenantID     uuid.UUID `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant       Tenant    `gorm:"foreignKey:TenantID" json:"-"`
	Email        string    `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"not null" json:"-"`
	Name         string    `gorm:"not null" json:"name"`
	Role         string    `gorm:"default:'cashier'" json:"role"` // owner, manager, cashier
	IsActive     bool      `gorm:"default:true" json:"is_active"`
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
	TenantID   uuid.UUID  `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant     Tenant     `gorm:"foreignKey:TenantID" json:"-"`
	CategoryID *uuid.UUID `gorm:"type:uuid" json:"category_id"`
	Category   *Category  `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Name       string     `gorm:"not null" json:"name"`
	SKU        string     `json:"sku"`
	Price      float64    `gorm:"not null" json:"price"`
	Cost       float64    `json:"cost"`
	StockQty   int        `gorm:"default:0" json:"stock_qty"`
	ImageURL   string     `json:"image_url"`
	IsActive   bool       `gorm:"default:true" json:"is_active"`
}

// RawMaterial represents raw materials/ingredients
type RawMaterial struct {
	BaseModel
	TenantID  uuid.UUID `gorm:"type:uuid;not null" json:"tenant_id"`
	Tenant    Tenant    `gorm:"foreignKey:TenantID" json:"-"`
	Name      string    `gorm:"not null" json:"name"`
	Unit      string    `gorm:"not null" json:"unit"` // kg, liter, pcs, etc.
	UnitPrice float64   `json:"unit_price"`
	StockQty  float64   `gorm:"default:0" json:"stock_qty"`
	Supplier  string    `json:"supplier"`
}

// ProductMaterial links products to raw materials
type ProductMaterial struct {
	ID           uuid.UUID   `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	ProductID    uuid.UUID   `gorm:"type:uuid;not null" json:"product_id"`
	Product      Product     `gorm:"foreignKey:ProductID" json:"-"`
	MaterialID   uuid.UUID   `gorm:"type:uuid;not null" json:"material_id"`
	Material     RawMaterial `gorm:"foreignKey:MaterialID" json:"material"`
	QuantityUsed float64     `gorm:"not null" json:"quantity_used"`
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
	InvoiceNumber string            `gorm:"uniqueIndex;not null" json:"invoice_number"`
	UserID        uuid.UUID         `gorm:"type:uuid;not null" json:"user_id"`
	User          User              `gorm:"foreignKey:UserID" json:"user,omitempty"`
	CustomerID    *uuid.UUID        `gorm:"type:uuid" json:"customer_id"`
	Customer      *Customer         `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Items         []TransactionItem `gorm:"foreignKey:TransactionID" json:"items"`
	Subtotal      float64           `gorm:"not null" json:"subtotal"`
	Discount      float64           `gorm:"default:0" json:"discount"`
	Tax           float64           `gorm:"default:0" json:"tax"`
	Total         float64           `gorm:"not null" json:"total"`
	Status        string            `gorm:"default:'completed'" json:"status"` // completed, voided
	PaymentMethod string            `gorm:"default:'cash'" json:"payment_method"`
}

// TransactionItem represents items in a transaction
type TransactionItem struct {
	ID            uuid.UUID   `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TransactionID uuid.UUID   `gorm:"type:uuid;not null" json:"transaction_id"`
	ProductID     uuid.UUID   `gorm:"type:uuid;not null" json:"product_id"`
	Product       Product     `gorm:"foreignKey:ProductID" json:"product"`
	Quantity      int         `gorm:"not null" json:"quantity"`
	UnitPrice     float64     `gorm:"not null" json:"unit_price"`
	Subtotal      float64     `gorm:"not null" json:"subtotal"`
}

// Migrate runs database migrations
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Tenant{},
		&User{},
		&Category{},
		&Product{},
		&RawMaterial{},
		&ProductMaterial{},
		&Customer{},
		&Transaction{},
		&TransactionItem{},
	)
}
