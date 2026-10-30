package engines

import (
	"database/sql"
	"fmt"
)

// Stage 47.16.4b (G2 of docs/security/gst-companies-act-controls.md): ledger
// rows the database will not change.
//
// db/migrations_stage47_16_4b_gl_postings_guard.sql puts a trigger on every
// client's gl_postings: a posted row is never deleted, and its account,
// amounts, date, currency and keys never change - a mistake is corrected by a
// reversing entry. Two writes are sanctioned, each by naming itself for the
// length of its own transaction (SET LOCAL, so it ends with the transaction):
//
//   - "purge": deleting rows. Only a client's data reset does it
//     (ResetTenantData, operator-run). A sandbox reset TRUNCATEs, which a
//     row trigger does not see; a purged client's schema is dropped.
//   - "recode": a master renumber (recodeInSchemaTx) carries a renumbered
//     store, entity, cost center, department or project code onto the rows
//     that name it - the reference columns only, never money or dates.
//
// The bank-reconciliation match mark (matched_statement_line_id) is the one
// column any write may change. The trigger is deliberately not named
// "append_only": recodeTargets skips such tables entirely, and the ledger's
// reference columns must follow a renumber. A client's schema gets the
// trigger from the schema sync (db/tenant_schema_sync.go guardTriggers),
// which runs inside ProvisionTenantSchema and after every migrate.
//
// This is a guard against the application's own mistakes and casual edits,
// as the stored_value_ledger trigger is. A database owner can still disable a
// trigger; the audit trail covers that.

// sanctionedWriteSetting is the transaction-local setting a sanctioned write
// names itself with. The tests' connections carry "purge" (testConnStr), so a
// test cleanup can still delete the rows it posted.
const sanctionedWriteSetting = "erp.sanctioned_write"

// Sanctioned writes.
const (
	sanctionPurge  = "purge"
	sanctionRecode = "recode"
)

// sanctionWriteTx names this transaction's sanctioned write.
func sanctionWriteTx(tx *sql.Tx, kind string) error {
	if _, err := tx.Exec(`SELECT set_config($1, $2, true)`, sanctionedWriteSetting, kind); err != nil {
		return fmt.Errorf("could not sanction the %s: %v", kind, err)
	}
	return nil
}
