package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type ImportResult struct {
	TotalRows   int                  `json:"total_rows"`
	SuccessRows int                  `json:"success_rows"`
	FailedRows  int                  `json:"failed_rows"`
	Errors      []RowValidationError `json:"errors"`
}

type RowValidationError struct {
	RowNumber int    `json:"row_number"`
	Message   string `json:"message"`
}

// BulkImportCSV parses a CSV body, validates constraints, and inserts valid records inside a transaction
func BulkImportCSV(tenantID string, doctype string, r io.Reader, userID string) (*ImportResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV is empty or missing data rows")
	}

	headers := records[0]
	// Clean headers
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.ToLower(h))
	}

	result := &ImportResult{
		TotalRows: len(records) - 1,
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	for idx, row := range records[1:] {
		rowNumber := idx + 2
		docData := make(map[string]interface{})

		// Map headers to CSV values
		for colIdx, val := range row {
			if colIdx < len(headers) {
				fieldName := headers[colIdx]
				docData[fieldName] = strings.TrimSpace(val)
			}
		}

		// Perform field structure validation
		valErr := ValidateDocument(tenantID, doctype, docData)
		if valErr != nil {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   valErr.Error(),
			})
			continue
		}

		// Enforce unique constraints or generate ID if not supplied
		idVal, exists := docData["id"]
		var id string
		if exists && fmt.Sprintf("%v", idVal) != "" {
			id = fmt.Sprintf("%v", idVal)
		} else {
			// Generate dynamic sequence code or fallback uuid
			seqCode, seqErr := GenerateSequence(tenantID, doctype, "HQ", time.Now().Format("2006"))
			if seqErr != nil {
				// Fallback to random generator if prefix counter doesn't exist
				id = "REC" + fmt.Sprintf("%d", time.Now().UnixNano())
			} else {
				id = seqCode
			}
		}

		// Marshall data column
		marshaled, mErr := json.Marshal(docData)
		if mErr != nil {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   fmt.Sprintf("Failed to marshal JSON payload: %v", mErr),
			})
			continue
		}

		// Insert document record
		query := fmt.Sprintf(`
			INSERT INTO documents (id, doctype, data, status, created_by) 
			VALUES ($1, $2, $3, $4, $5) 
			ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = CURRENT_TIMESTAMP`)
		_, execErr := tx.Exec(query, id, doctype, marshaled, "Active", userID)
		if execErr != nil {
			result.FailedRows++
			result.Errors = append(result.Errors, RowValidationError{
				RowNumber: rowNumber,
				Message:   fmt.Sprintf("Database write error: %v", execErr),
			})
			continue
		}

		result.SuccessRows++
	}

	// Commit transaction if there are any successful rows inserted
	if result.SuccessRows > 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// GenerateCSVTemplate returns a dummy CSV buffer containing the headers for a doctype schema
func GenerateCSVTemplate(tenantID string, doctype string) ([]byte, error) {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	var headers []string
	// Append ID always as the first column indicator
	headers = append(headers, "id")
	for _, f := range fields {
		if f.Fieldname == "id" || f.Fieldname == "status" {
			continue
		}
		headers = append(headers, f.Fieldname)
	}

	if err := writer.Write(headers); err != nil {
		return nil, err
	}
	writer.Flush()
	return buf.Bytes(), nil
}
