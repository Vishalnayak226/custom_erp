package engines

import (
	"custom_erp/db"
	"fmt"
)

type FieldPermission struct {
	AllowRead  bool
	AllowWrite bool
}

func fieldPermissions(tenantID, role, doctype string) (map[string]FieldPermission, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT fieldname, allow_read, allow_write FROM %s.field_permissions WHERE role=$1 AND doctype_name=$2`, schema), role, doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]FieldPermission{}
	for rows.Next() {
		var field string
		var p FieldPermission
		if err := rows.Scan(&field, &p.AllowRead, &p.AllowWrite); err != nil {
			return nil, err
		}
		result[field] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Stage 47.1.3: merge the code-declared sensitive-field policy
	// (sensitive_fields.go) on top of the tenant's own rows. This is the
	// single choke point every field-permission caller already runs
	// through - read, form meta, write, CSV import and PIM bulk edit - so
	// the policy needs no call-site change anywhere.
	//
	// The merge only ever TIGHTENS: a tenant row that already denies stays
	// denying, and a tenant cannot grant a role access to payroll, bank
	// details or a connector secret by inserting an allow row. Before this,
	// a doctype with no rows meant every field was readable by every role
	// that passed the doctype gate.
	for field, policy := range sensitiveFieldPolicy(role, doctype) {
		existing, ok := result[field]
		if !ok {
			result[field] = policy
			continue
		}
		existing.AllowRead = existing.AllowRead && policy.AllowRead
		existing.AllowWrite = existing.AllowWrite && policy.AllowWrite
		result[field] = existing
	}
	return result, nil
}

func FilterFieldsForRole(tenantID, role, doctype string, data map[string]interface{}) (map[string]interface{}, error) {
	permissions, err := fieldPermissions(tenantID, role, doctype)
	if err != nil {
		return nil, err
	}
	for field, permission := range permissions {
		if !permission.AllowRead {
			delete(data, field)
		}
	}
	return data, nil
}

func RejectRestrictedFieldWrites(tenantID, role, doctype string, data map[string]interface{}) error {
	permissions, err := fieldPermissions(tenantID, role, doctype)
	if err != nil {
		return err
	}
	for field := range data {
		if permission, exists := permissions[field]; exists && !permission.AllowWrite {
			return fmt.Errorf("field %q is not writable for role %s", field, role)
		}
	}
	return nil
}

func FilterFieldMetaForRole(tenantID, role, doctype string, fields []FieldMeta) ([]FieldMeta, error) {
	permissions, err := fieldPermissions(tenantID, role, doctype)
	if err != nil {
		return nil, err
	}
	out := make([]FieldMeta, 0, len(fields))
	for _, field := range fields {
		if permission, exists := permissions[field.Fieldname]; exists && !permission.AllowRead {
			continue
		}
		out = append(out, field)
	}
	return out, nil
}
