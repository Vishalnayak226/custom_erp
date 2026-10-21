package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 74.1.5 (BRD §2.1-2.2, §8.1, §8.3, NFR-01/02/06): the one way a new
// economic document posts - store credit, material issue / return, stock
// adjustment, challans (74.2-74.3). Built on what exists:
//
//   - ClaimCommand idempotency: a replay returns the first result; the same
//     key with a different payload is a 409;
//   - the document's revision (documents.version) against the caller's
//     expected_version: a stale edit is a 409;
//   - the document row locked FOR UPDATE for the whole posting, which the
//     posting itself recomputes from the stored lines, never the request;
//   - the stock ledger written in the same transaction as availability and
//     the GL (WriteStockLedgerLinesTx), refusing a closed period - so a
//     posted document is either wholly there or not at all.
//
// The poster on every GL row is already stamped by PostDoubleEntryTx (73.1.9).

// EconomicCommand is one posting of one document.
type EconomicCommand struct {
	Command         string      // e.g. "inventory.adjustment.post"
	Doctype         string      // the document's doctype
	DocID           string      // its id
	IdempotencyKey  string      // the caller's key; "" = no replay protection
	ExpectedVersion int         // the revision the caller saw; 0 = not checked
	Payload         interface{} // the request, for the replay digest
	CorrelationID   string
}

// EconomicPost does the posting inside the command's transaction; doc is the
// locked document's data. It returns the response to store for a replay.
type EconomicPost func(tx *sql.Tx, schema string, doc map[string]interface{}) (map[string]interface{}, error)

// RunEconomicCommand runs post once, all or nothing.
func RunEconomicCommand(tenantID string, c EconomicCommand, post EconomicPost) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	claimKey := ""
	if strings.TrimSpace(c.IdempotencyKey) != "" {
		claim, err := ClaimCommand(tenantID, c.Command, c.IdempotencyKey, CommandDigest(c.Payload), c.CorrelationID)
		if err != nil {
			return nil, err
		}
		switch claim.Outcome {
		case ClaimReplay:
			return claim.Response, nil
		case ClaimInProgress:
			return nil, &ValidationError{Code: "GLOBAL-0006", Message: "this posting is already running - wait a moment and look again"}
		case ClaimPayloadMismatch:
			return nil, &ValidationError{Code: "GLOBAL-0006", Message: "this request key was already used for a different request"}
		}
		claimKey = claim.Key
	}
	fail := func(err error) (map[string]interface{}, error) {
		if claimKey != "" {
			FailCommand(tenantID, claimKey, err.Error())
		}
		return nil, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return fail(err)
	}
	var raw string
	var version int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, COALESCE(version, 1) FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema),
		c.Doctype, c.DocID).Scan(&raw, &version); err != nil {
		if err == sql.ErrNoRows {
			return fail(&ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("%s %s was not found", c.Doctype, c.DocID)})
		}
		return fail(err)
	}
	if c.ExpectedVersion > 0 && c.ExpectedVersion != version {
		return fail(&ValidationError{Code: "GLOBAL-0006", Message: fmt.Sprintf("%s %s was changed by someone else (revision %d, you had %d) - reload it and try again", c.Doctype, c.DocID, version, c.ExpectedVersion)})
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return fail(err)
	}
	response, err := post(tx, schema, doc)
	if err != nil {
		return fail(err)
	}
	if response == nil {
		response = map[string]interface{}{}
	}
	if claimKey != "" {
		if err := CompleteCommandTx(tx, schema, claimKey, c.DocID, response); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return response, nil
}

// WriteStockLedgerLinesTx writes the ledger entries for movements inside the
// caller's transaction (74.1.5), with the same merge and idempotency keys as
// WriteStockLedgerLines - and refuses a posting dated in a closed accounting
// period (74.0.2 c), as the GL does. date is the posting's own date
// (YYYY-MM-DD); "" = today. The existing post-commit WriteStockLedgerLines
// stays for today's callers until each is moved deliberately.
func WriteStockLedgerLinesTx(tx *sql.Tx, schema, locationCode, voucherType, voucherID, userID, date string, lines []PostedStockLine) error {
	if err := rejectIfCurrentPeriodClosed(tx, schema, voucherType, voucherID, date); err != nil {
		return err
	}
	for _, e := range stockLedgerEntriesFor(locationCode, voucherType, voucherID, userID, lines) {
		if err := insertStockLedgerEntry(tx, schema, e); err != nil {
			return err
		}
	}
	return nil
}
