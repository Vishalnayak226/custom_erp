package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// GenerateSequence generates the next document code using row-level locking
func GenerateSequence(tenantID, docType, storeCode, financialYear string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	// 1. Fetch prefix configuration for this document type
	var prefix, separator, resetFreq string
	var paddingWidth int
	var activeStatus bool
	queryConfig := `SELECT prefix, separator, padding_width, reset_frequency, active_status 
	                FROM prefix_configs WHERE doc_type = $1`
	err = tx.QueryRow(queryConfig, docType).Scan(&prefix, &separator, &paddingWidth, &resetFreq, &activeStatus)
	if err == sql.ErrNoRows {
		// Use default fallbacks
		prefix = docType
		separator = "/"
		paddingWidth = 6
		resetFreq = "ANNUAL"
		activeStatus = true
	} else if err != nil {
		return "", err
	}

	if !activeStatus {
		return "", fmt.Errorf("numbering configuration for %s is inactive", docType)
	}

	// 2. Fetch or create counter for store and financial year with row lock
	var currentVal int64
	queryCounter := `SELECT current_val FROM sequence_counters 
	                 WHERE doc_type = $1 AND store_code = $2 AND financial_year = $3 
	                 FOR UPDATE`
	err = tx.QueryRow(queryCounter, docType, storeCode, financialYear).Scan(&currentVal)
	if err == sql.ErrNoRows {
		currentVal = 0
		insertCounter := `INSERT INTO sequence_counters (doc_type, store_code, financial_year, current_val) 
		                  VALUES ($1, $2, $3, $4)`
		_, err = tx.Exec(insertCounter, docType, storeCode, financialYear, currentVal)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	// 3. Increment counter
	nextVal := currentVal + 1
	updateCounter := `UPDATE sequence_counters SET current_val = $1 
	                  WHERE doc_type = $2 AND store_code = $3 AND financial_year = $4`
	_, err = tx.Exec(updateCounter, nextVal, docType, storeCode, financialYear)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	// 4. Format sequence code
	formatStr := fmt.Sprintf("%%0%dd", paddingWidth)
	paddedNum := fmt.Sprintf(formatStr, nextVal)

	// Format: <Prefix><Separator><StoreCode><Separator><FinancialYear><Separator><PaddedNum>
	parts := []string{}
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if storeCode != "" {
		parts = append(parts, storeCode)
	}
	if financialYear != "" {
		parts = append(parts, financialYear)
	}
	parts = append(parts, paddedNum)

	return strings.Join(parts, separator), nil
}

// GenerateVariantCode constructs a variant/child identifier from the parent code and attribute values using a template format pattern
func GenerateVariantCode(tenantID string, parentCode string, pattern string, attributes map[string]string) string {
	if pattern == "" {
		// Fallback default concatenation: ParentCode-AttrVal1-AttrVal2
		var parts []string
		parts = append(parts, parentCode)
		for _, v := range attributes {
			if v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, "-")
	}

	result := pattern
	result = strings.ReplaceAll(result, "{Parent}", parentCode)
	for k, v := range attributes {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}
