package inventory

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"github.com/yuditriaji/warungin-backend/pkg/database"
	"gorm.io/gorm"
)

type ImportHandler struct {
	db *gorm.DB
}

func NewImportHandler(db *gorm.DB) *ImportHandler {
	return &ImportHandler{db: db}
}

type ImportResult struct {
	TotalRows    int      `json:"total_rows"`
	SuccessCount int      `json:"success_count"`
	FailedCount  int      `json:"failed_count"`
	Errors       []string `json:"errors"`
}

type ImportRow struct {
	ProductName string
	SKU         string
	StockQty    int
	Price       float64
	Cost        float64
}

// ImportExcel handles Excel/CSV file upload for bulk inventory import
func (h *ImportHandler) ImportExcel(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tenantUUID, _ := uuid.Parse(tenantID)
	
	// Get outlet_id if provided
	outletIDStr := c.PostForm("outlet_id")
	var outletID *uuid.UUID
	if outletIDStr != "" {
		parsed, err := uuid.Parse(outletIDStr)
		if err == nil {
			outletID = &parsed
		}
	}

	// Get uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}
	defer file.Close()

	// Parse file based on extension
	var rows []ImportRow
	fileName := strings.ToLower(header.Filename)
	
	if strings.HasSuffix(fileName, ".xlsx") || strings.HasSuffix(fileName, ".xls") {
		rows, err = h.parseExcel(file)
	} else if strings.HasSuffix(fileName, ".csv") {
		rows, err = h.parseCSV(file)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported file format. Please upload .xlsx or .csv"})
		return
	}

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to parse file: %v", err)})
		return
	}

	// Process rows
	result := ImportResult{
		TotalRows: len(rows),
		Errors:    []string{},
	}

	for i, row := range rows {
		if row.ProductName == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("Row %d: Product name is required", i+2))
			result.FailedCount++
			continue
		}

		// Check if product exists by SKU or name
		var existingProduct database.Product
		var found bool

		if row.SKU != "" {
			if err := h.db.Where("tenant_id = ? AND sku = ?", tenantID, row.SKU).First(&existingProduct).Error; err == nil {
				found = true
			}
		}

		if !found {
			if err := h.db.Where("tenant_id = ? AND name = ?", tenantID, row.ProductName).First(&existingProduct).Error; err == nil {
				found = true
			}
		}

		if found {
			// Update existing product stock
			updates := map[string]interface{}{
				"stock_qty": row.StockQty,
			}
			if row.Price > 0 {
				updates["price"] = row.Price
			}
			if row.Cost > 0 {
				updates["cost"] = row.Cost
			}

			if err := h.db.Model(&existingProduct).Updates(updates).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Row %d: Failed to update %s - %v", i+2, row.ProductName, err))
				result.FailedCount++
				continue
			}
			result.SuccessCount++
		} else {
			// Create new product
			newProduct := database.Product{
				TenantID: tenantUUID,
				OutletID: outletID,
				Name:     row.ProductName,
				SKU:      row.SKU,
				StockQty: row.StockQty,
				Price:    row.Price,
				Cost:     row.Cost,
				IsActive: true,
			}

			if err := h.db.Create(&newProduct).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Row %d: Failed to create %s - %v", i+2, row.ProductName, err))
				result.FailedCount++
				continue
			}
			result.SuccessCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":    result,
		"message": fmt.Sprintf("Import completed: %d success, %d failed", result.SuccessCount, result.FailedCount),
	})
}

// parseExcel parses .xlsx files
func (h *ImportHandler) parseExcel(file io.Reader) ([]ImportRow, error) {
	f, err := excelize.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Get first sheet
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets found in file")
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("file must have header row and at least one data row")
	}

	// Find column indices from header
	header := rows[0]
	colMap := make(map[string]int)
	for i, cell := range header {
		normalized := strings.ToLower(strings.TrimSpace(cell))
		colMap[normalized] = i
	}

	// Parse data rows
	var result []ImportRow
	for _, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}

		importRow := ImportRow{}

		// Product Name (required) - check common column names
		for _, col := range []string{"nama produk", "product name", "nama", "name", "produk"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				importRow.ProductName = strings.TrimSpace(row[idx])
				break
			}
		}

		// SKU
		for _, col := range []string{"sku", "kode", "code", "kode produk"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				importRow.SKU = strings.TrimSpace(row[idx])
				break
			}
		}

		// Stock Qty
		for _, col := range []string{"stok", "stock", "qty", "jumlah", "stock qty"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				if val, err := strconv.Atoi(strings.TrimSpace(row[idx])); err == nil {
					importRow.StockQty = val
				}
				break
			}
		}

		// Price
		for _, col := range []string{"harga", "price", "harga jual"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				if val, err := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64); err == nil {
					importRow.Price = val
				}
				break
			}
		}

		// Cost
		for _, col := range []string{"modal", "cost", "harga modal", "cogs"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				if val, err := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64); err == nil {
					importRow.Cost = val
				}
				break
			}
		}

		if importRow.ProductName != "" {
			result = append(result, importRow)
		}
	}

	return result, nil
}

// parseCSV parses .csv files
func (h *ImportHandler) parseCSV(file io.Reader) ([]ImportRow, error) {
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("file must have header row and at least one data row")
	}

	// Find column indices from header
	header := records[0]
	colMap := make(map[string]int)
	for i, cell := range header {
		normalized := strings.ToLower(strings.TrimSpace(cell))
		colMap[normalized] = i
	}

	// Parse data rows (same logic as Excel)
	var result []ImportRow
	for _, row := range records[1:] {
		if len(row) == 0 {
			continue
		}

		importRow := ImportRow{}

		// Product Name
		for _, col := range []string{"nama produk", "product name", "nama", "name", "produk"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				importRow.ProductName = strings.TrimSpace(row[idx])
				break
			}
		}

		// SKU
		for _, col := range []string{"sku", "kode", "code", "kode produk"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				importRow.SKU = strings.TrimSpace(row[idx])
				break
			}
		}

		// Stock Qty
		for _, col := range []string{"stok", "stock", "qty", "jumlah", "stock qty"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				if val, err := strconv.Atoi(strings.TrimSpace(row[idx])); err == nil {
					importRow.StockQty = val
				}
				break
			}
		}

		// Price
		for _, col := range []string{"harga", "price", "harga jual"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				if val, err := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64); err == nil {
					importRow.Price = val
				}
				break
			}
		}

		// Cost
		for _, col := range []string{"modal", "cost", "harga modal", "cogs"} {
			if idx, ok := colMap[col]; ok && idx < len(row) {
				if val, err := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64); err == nil {
					importRow.Cost = val
				}
				break
			}
		}

		if importRow.ProductName != "" {
			result = append(result, importRow)
		}
	}

	return result, nil
}

// DownloadTemplate generates a sample Excel template for import
func (h *ImportHandler) DownloadTemplate(c *gin.Context) {
	f := excelize.NewFile()
	defer f.Close()

	// Create header
	headers := []string{"Nama Produk", "SKU", "Stok", "Harga", "Modal"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue("Sheet1", cell, header)
	}

	// Add sample data
	sampleData := [][]interface{}{
		{"Nasi Goreng", "NG-001", 100, 15000, 8000},
		{"Es Teh Manis", "ETM-001", 50, 5000, 2000},
		{"Ayam Geprek", "AG-001", 30, 20000, 12000},
	}

	for rowIdx, row := range sampleData {
		for colIdx, value := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			f.SetCellValue("Sheet1", cell, value)
		}
	}

	// Set column widths
	f.SetColWidth("Sheet1", "A", "A", 20)
	f.SetColWidth("Sheet1", "B", "B", 15)
	f.SetColWidth("Sheet1", "C", "E", 12)

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=template_import_stok.xlsx")

	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate template"})
		return
	}
}
