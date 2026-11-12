package engines

import (
	"database/sql"
	"fmt"
	"strings"
)

// Stage 148.46: an OMS bill carries the order's shipping charge exactly as the
// pack-time ERP bill does (148.2) - GST-inclusive, on the first package of the
// order only. The OMS bill's number is the GST invoice number, so its printed
// total and the ERP bill raised from it agree: RaiseERPBillForPackage copies
// the figures and the shipping fields, it never adds shipping again. Bills
// already raised are not rewritten, and an order that already has an OMS bill
// (shipping or not) gives later packages no shipping.
func addOMSBillShipping(tx *sql.Tx, schema, orderID string, billData map[string]interface{}, bill *packageBill) error {
	if strings.TrimSpace(orderID) == "" {
		return nil
	}
	var earlier bool
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'OMSBill' AND deleted_at IS NULL AND status <> 'Cancelled'
		   AND data->>'sales_order_id' = $1)`, schema), orderID).Scan(&earlier); err != nil {
		return err
	}
	if earlier {
		return nil
	}
	return addPackageShipping(tx, schema, orderID, billData, bill)
}
