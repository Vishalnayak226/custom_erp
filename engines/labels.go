package engines

import (
	"custom_erp/db"
)

// GetLabels retrieves all dynamic label replacements for the tenant
func GetLabels(tenantID string) (map[string]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	rows, err := tx.Query("SELECT original_text, custom_text FROM dynamic_labels")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labels := make(map[string]string)
	for rows.Next() {
		var orig, custom string
		if err := rows.Scan(&orig, &custom); err != nil {
			return nil, err
		}
		labels[orig] = custom
	}

	return labels, tx.Commit()
}

// SaveLabel adds or updates a label translation
func SaveLabel(tenantID, originalText, customText string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	query := `INSERT INTO dynamic_labels (original_text, custom_text) 
	          VALUES ($1, $2)
	          ON CONFLICT (original_text) DO UPDATE SET custom_text = EXCLUDED.custom_text`
	_, err = tx.Exec(query, originalText, customText)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteLabel removes a label translation mapping
func DeleteLabel(tenantID, originalText string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM dynamic_labels WHERE original_text = $1", originalText)
	if err != nil {
		return err
	}

	return tx.Commit()
}
