package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type FieldMeta struct {
	ID           string `json:"id"`
	DocTypeName  string `json:"doctype_name"`
	Fieldname    string `json:"fieldname"`
	Label        string `json:"label"`
	Fieldtype    string `json:"fieldtype"`
	Mandatory    bool   `json:"mandatory"`
	Options      string `json:"options"`
	DisplayOrder int    `json:"display_order"`
}

// GetDocTypeMeta retrieves the fields definition metadata for a doctype
func GetDocTypeMeta(tenantID string, doctype string) ([]FieldMeta, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, doctype_name, fieldname, label, fieldtype, mandatory, COALESCE(options, ''), display_order 
		FROM %s.doctype_fields 
		WHERE doctype_name = $1
		ORDER BY display_order ASC`, schema), doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []FieldMeta
	for rows.Next() {
		var f FieldMeta
		err := rows.Scan(&f.ID, &f.DocTypeName, &f.Fieldname, &f.Label, &f.Fieldtype, &f.Mandatory, &f.Options, &f.DisplayOrder)
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// SaveFieldDefinition adds or updates a field definition in a doctype metadata
func SaveFieldDefinition(tenantID string, doctype string, f FieldMeta) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (doctype_name, fieldname) DO UPDATE SET 
			label = EXCLUDED.label, 
			fieldtype = EXCLUDED.fieldtype, 
			mandatory = EXCLUDED.mandatory, 
			options = EXCLUDED.options, 
			display_order = EXCLUDED.display_order`, schema)

	var opts interface{}
	if f.Options != "" {
		opts = f.Options
	} else {
		opts = nil
	}

	_, err = db.DB.Exec(query, doctype, f.Fieldname, f.Label, f.Fieldtype, f.Mandatory, opts, f.DisplayOrder)
	return err
}

// DeleteFieldDefinition removes a field definition from metadata
func DeleteFieldDefinition(tenantID string, doctype string, fieldID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf("DELETE FROM %s.doctype_fields WHERE doctype_name = $1 AND id = $2", schema), doctype, fieldID)
	return err
}

// GetDocTypes returns all registered doctypes
func GetDocTypes(tenantID string) ([]map[string]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT name, module, COALESCE(document_type, 'Master') FROM %s.doctype_meta ORDER BY name", schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []map[string]string
	for rows.Next() {
		var name, module, docType string
		if err := rows.Scan(&name, &module, &docType); err != nil {
			return nil, err
		}
		list = append(list, map[string]string{
			"name":          name,
			"module":        module,
			"document_type": docType,
		})
	}
	return list, nil
}

// SaveDocType registers a new doctype definition in doctype_meta
func SaveDocType(tenantID string, name string, module string, docType string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.doctype_meta (name, module, document_type) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (name) DO UPDATE SET 
			module = EXCLUDED.module, 
			document_type = EXCLUDED.document_type`, schema)
	_, err = db.DB.Exec(query, name, module, docType)
	return err
}

// ValidateDocument checks document properties against doctype_fields rules
func ValidateDocument(tenantID string, doctype string, docData map[string]interface{}) error {
	fields, err := GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		return err
	}

	if len(fields) == 0 {
		return fmt.Errorf("doctype %s metadata fields not found", doctype)
	}

	for _, f := range fields {
		val, exists := docData[f.Fieldname]
		valStr := ""
		if exists && val != nil {
			valStr = strings.TrimSpace(fmt.Sprintf("%v", val))
		}

		// 1. Mandatory check
		if f.Mandatory && valStr == "" {
			return fmt.Errorf("Field %q (%s) is required", f.Label, f.Fieldname)
		}

		// 2. Type/Format check
		if valStr != "" {
			switch f.Fieldtype {
			case "Number":
				var num float64
				_, err := fmt.Sscanf(valStr, "%f", &num)
				if err != nil {
					return fmt.Errorf("Field %q must be a valid number", f.Label)
				}
			case "Select":
				if f.Options != "" {
					allowed := strings.Split(f.Options, ",")
					matched := false
					for _, o := range allowed {
						if strings.TrimSpace(o) == valStr {
							matched = true
							break
						}
					}
					if !matched {
						return fmt.Errorf("Field %q value %q is not in allowed list (%s)", f.Label, valStr, f.Options)
					}
				}
			case "Link":
				if f.Options != "" { // options store target Doctype
					existsLink, err := verifyDocumentExists(tenantID, f.Options, valStr)
					if err != nil {
						return err
					}
					if !existsLink {
						return fmt.Errorf("Linked %s record with ID %q does not exist", f.Options, valStr)
					}
				}
			}
		}
	}

	return nil
}

func verifyDocumentExists(tenantID, doctype, id string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s.documents 
			WHERE doctype = $1 AND id = $2 AND status != 'Cancelled'
		)`, schema), doctype, id).Scan(&exists)
	return exists, err
}

type IndustryProfile struct {
	IndustryCode     string            `json:"industry_code"`
	IndustryName     string            `json:"industry_name"`
	DocTypeOverrides []DocTypeOverride `json:"doctype_overrides"`
	Labels           map[string]string `json:"labels"`
}

type DocTypeOverride struct {
	Name         string      `json:"name"`
	Module       string      `json:"module"`
	DocumentType string      `json:"document_type"`
	NewLabel     string      `json:"new_label"`
	Fields       []FieldMeta `json:"fields"`
}

// SwitchIndustryProfile re-configures the dynamic doctypes, fields, and label translations based on an industry JSON configuration profile
func SwitchIndustryProfile(tenantID string, profilePath string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("failed to read profile path %s: %w", profilePath, err)
	}

	var prof IndustryProfile
	if err := json.Unmarshal(data, &prof); err != nil {
		return fmt.Errorf("failed to unmarshal profile JSON: %w", err)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// 1. Re-register doctypes and upsert field presets.
	// Deliberately does NOT delete existing fields first: industry profile JSON files are
	// partial overlays declaring the industry-specific additions/overrides for a doctype,
	// not its complete field list - none of the shipped profiles (jewelry/food_bev/auto/
	// clothing) redeclare base fields like "status". A prior delete-then-insert here silently
	// dropped any field a profile didn't mention on every single industry switch.
	for _, o := range prof.DocTypeOverrides {
		// Insert or update doctype meta
		metaQuery := fmt.Sprintf(`
			INSERT INTO doctype_meta (name, module, document_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (name) DO UPDATE SET
				module = EXCLUDED.module,
				document_type = EXCLUDED.document_type`)
		_, err = tx.Exec(metaQuery, o.Name, o.Module, o.DocumentType)
		if err != nil {
			return fmt.Errorf("failed to register doctype %s: %w", o.Name, err)
		}

		// Upsert the profile's field presets - fields not mentioned by this profile are left untouched
		for _, f := range o.Fields {
			var opts interface{}
			if f.Options != "" {
				opts = f.Options
			}
			fieldQuery := fmt.Sprintf(`
				INSERT INTO doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
					label = EXCLUDED.label,
					fieldtype = EXCLUDED.fieldtype,
					mandatory = EXCLUDED.mandatory,
					options = EXCLUDED.options,
					display_order = EXCLUDED.display_order`)
			_, err = tx.Exec(fieldQuery, o.Name, f.Fieldname, f.Label, f.Fieldtype, f.Mandatory, opts, f.DisplayOrder)
			if err != nil {
				return fmt.Errorf("failed to save field %s for %s: %w", f.Fieldname, o.Name, err)
			}
		}
	}

	// 2. Clear old dynamic labels and insert custom translation maps
	_, _ = tx.Exec("DELETE FROM dynamic_labels")
	for orig, custom := range prof.Labels {
		labelQuery := fmt.Sprintf(`
			INSERT INTO dynamic_labels (original_text, custom_text) 
			VALUES ($1, $2)
			ON CONFLICT (original_text) DO UPDATE SET custom_text = EXCLUDED.custom_text`)
		_, err = tx.Exec(labelQuery, orig, custom)
		if err != nil {
			return fmt.Errorf("failed to save dynamic label %s: %w", orig, err)
		}
	}

	// 3. Log audit event
	auditQuery := fmt.Sprintf(`
		INSERT INTO audit_logs (user_id, action, status, details) 
		VALUES ($1, $2, $3, $4)`)
	auditDetails := fmt.Sprintf("Switched active industry profile to: %s (%s)", prof.IndustryName, prof.IndustryCode)
	_, err = tx.Exec(auditQuery, "admin", "SWITCH_INDUSTRY", "SUCCESS", auditDetails)
	if err != nil {
		return err
	}

	return tx.Commit()
}
