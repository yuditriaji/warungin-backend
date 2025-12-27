# Warungin Backend

Go-based REST API for Warungin POS SaaS platform.

## Tech Stack

- **Go 1.22+**
- **Gin** - HTTP framework
- **GORM** - ORM
- **PostgreSQL** - Database
- **JWT** - Authentication

## Getting Started

### Prerequisites

- Go 1.22+
- PostgreSQL 15+

### Installation

1. Clone the repository:
```bash
git clone https://github.com/yuditriaji/warungin-backend.git
cd warungin-backend
```

2. Copy environment file:
```bash
cp .env.example .env
```

3. Update `.env` with your database credentials

4. Install dependencies:
```bash
go mod download
```

5. Run the server:
```bash
go run cmd/server/main.go
```

Server will start on `http://localhost:8080`

## API Endpoints

### Authentication
- `POST /api/v1/auth/register` - Register new business
- `POST /api/v1/auth/login` - Login
- `POST /api/v1/auth/refresh` - Refresh token

### Products (Protected)
- `GET /api/v1/products` - List products
- `POST /api/v1/products` - Create product
- `GET /api/v1/products/:id` - Get product
- `PUT /api/v1/products/:id` - Update product
- `DELETE /api/v1/products/:id` - Delete product

### Transactions (Protected)
- `GET /api/v1/transactions` - List transactions
- `POST /api/v1/transactions` - Create transaction
- `GET /api/v1/transactions/:id` - Get transaction

## Project Structure

```
├── cmd/
│   └── server/          # Application entrypoint
├── internal/
│   ├── auth/            # Authentication
│   ├── product/         # Product management
│   └── transaction/     # POS transactions
├── pkg/
│   ├── database/        # DB connection & models
│   └── middleware/      # HTTP middleware
├── migrations/          # SQL migrations
└── config/              # Configuration
```

## License

MIT
