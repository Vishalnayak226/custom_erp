package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"custom_erp/db"
)

// Stage 148.5: the chart of accounts maintained from the app. A GLAccountChange
// document (Add / Change / Block / Unblock) goes through the maker-checker
// approval engine and is applied when approved. There is no delete: gl_postings
// keeps a RESTRICT foreign key to the account, so an account with postings can
// never be removed, and a Block is how one is retired. Account codes never
// change - Change edits the name, type, parent and the no-manual-posting flag.

// GLAccountChangeDoctype is the change request's doctype name.
const GLAccountChangeDoctype = "GLAccountChange"

var glAccountCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,49}$`)

var glAccountTypes = []string{"Asset", "Liability", "Equity", "Revenue", "Expense"}

type glAccountFlags struct {
	Exists, Blocked, NoManual bool
	Name                      string
}

// glAccountFlagsQ reads the flags of the accounts named, schema-qualified so it
// works on a transaction or the pool.
func glAccountFlagsQ(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema string, codes []string) (map[string]glAccountFlags, error) {
	out := map[string]glAccountFlags{}
	if len(codes) == 0 {
		return out, nil
	}
	ph := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, c := range codes {
		ph[i], args[i] = fmt.Sprintf("$%d", i+1), c
	}
	rows, err := q.Query(fmt.Sprintf(`SELECT account_code, account_name, blocked, no_manual_posting FROM %s.gl_accounts WHERE account_code IN (%s)`,
		schema, strings.Join(ph, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		f := glAccountFlags{Exists: true}
		if err := rows.Scan(&code, &f.Name, &f.Blocked, &f.NoManual); err != nil {
			return nil, err
		}
		out[code] = f
	}
	return out, rows.Err()
}

// checkPostingAccountsTx is PostDoubleEntryTx's gate: every account a journal
// touches must be in the chart and not blocked. A cancellation (":CANCEL" key)
// is let through to a blocked account so an old document can still be undone;
// the account's history and balance are untouched by a block either way.
func checkPostingAccountsTx(tx *sql.Tx, schema, postingKey string, debits, credits map[string]int64) error {
	seen := map[string]bool{}
	var codes []string
	for _, m := range []map[string]int64{debits, credits} {
		for c, v := range m {
			if v > 0 && !seen[c] {
				seen[c] = true
				codes = append(codes, c)
			}
		}
	}
	flags, err := glAccountFlagsQ(tx, schema, codes)
	if err != nil {
		return err
	}
	cancel := strings.Contains(postingKey, ":CANCEL")
	for _, c := range codes {
		f, ok := flags[c]
		switch {
		case !ok:
			return &ValidationError{Code: "FIN-0871", SubFor: "Account", Message: fmt.Sprintf("account %s is not in the chart of accounts - nothing was posted", c)}
		case f.Blocked && !cancel:
			return &ValidationError{Code: "FIN-0870", SubFor: "Account", Message: fmt.Sprintf("account %s (%s) is blocked - nothing was posted; unblock it or point the posting rule at another account", c, f.Name)}
		}
	}
	return nil
}

// refuseManualPostingToControlAccounts is the manual journal's check: a line on
// a "no manual posting" account is refused. System postings never come here.
func refuseManualPostingToControlAccounts(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema string, lines []JournalVoucherLine) error {
	codes := make([]string, 0, len(lines))
	for _, l := range lines {
		codes = append(codes, l.AccountCode)
	}
	flags, err := glAccountFlagsQ(q, schema, codes)
	if err != nil {
		return err
	}
	for _, l := range lines {
		if f := flags[l.AccountCode]; f.NoManual {
			return &ValidationError{Code: "FIN-0872", SubFor: "Account", Message: fmt.Sprintf("account %s (%s) is a control account - it takes no manual journal; post through its own document", l.AccountCode, f.Name)}
		}
	}
	return nil
}

// validateGLAccountChange is the change request's save check.
func validateGLAccountChange(tenantID string, payload map[string]interface{}) error {
	bad := func(sub, msg string) error { return &ValidationError{Code: "FIN-0873", SubFor: sub, Message: msg} }
	action := strings.TrimSpace(strField(payload, "action"))
	code := strings.TrimSpace(strField(payload, "account_code"))
	if !containsString([]string{"Add", "Change", "Block", "Unblock"}, action) {
		return bad("Action", "choose Add, Change, Block or Unblock")
	}
	if !glAccountCodePattern.MatchString(code) {
		return bad("Account code", "use up to 50 letters, digits, dots, dashes or underscores")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	parent := strings.TrimSpace(strField(payload, "parent_code"))
	codes := []string{code}
	if parent != "" {
		codes = append(codes, parent)
	}
	flags, err := glAccountFlagsQ(db.DB, schema, codes)
	if err != nil {
		return err
	}
	if action == "Add" {
		if flags[code].Exists {
			return bad("Account code", fmt.Sprintf("account %s already exists - codes are never reused", code))
		}
		if strings.TrimSpace(strField(payload, "account_name")) == "" {
			return bad("Account name", "name the new account")
		}
		if !containsString(glAccountTypes, strField(payload, "account_type")) {
			return bad("Type", "choose the account type")
		}
	} else if !flags[code].Exists {
		return bad("Account code", fmt.Sprintf("account %s is not in the chart of accounts", code))
	}
	if t := strField(payload, "account_type"); t != "" && !containsString(glAccountTypes, t) {
		return bad("Type", "choose Asset, Liability, Equity, Revenue or Expense")
	}
	if parent != "" {
		if parent == code {
			return bad("Parent account", "an account cannot be its own parent")
		}
		if !flags[parent].Exists {
			return bad("Parent account", fmt.Sprintf("parent %s is not in the chart of accounts", parent))
		}
		// Walk up the parent chain: the code must not already be above the parent.
		cur := parent
		for i := 0; i < 50 && cur != ""; i++ {
			var next sql.NullString
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT parent_code FROM %s.gl_accounts WHERE account_code = $1`, schema), cur).Scan(&next); err != nil {
				break
			}
			if next.String == code {
				return bad("Parent account", "that parent sits under this account - the grouping would loop")
			}
			cur = next.String
		}
	}
	return nil
}

// applyGLAccountChange carries out an approved change once (applied_at marks it).
func applyGLAccountChange(tenantID, docID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1 AND doctype = $2 FOR UPDATE`, schema), docID, GLAccountChangeDoctype).Scan(&raw); err != nil {
		return err
	}
	var d map[string]interface{}
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	if strField(d, "applied_at") != "" {
		return nil
	}
	if err := validateGLAccountChange(tenantID, d); err != nil {
		return err
	}
	code := strings.TrimSpace(strField(d, "account_code"))
	name, typ := strings.TrimSpace(strField(d, "account_name")), strings.TrimSpace(strField(d, "account_type"))
	parent := strings.TrimSpace(strField(d, "parent_code"))
	flagArg := func() interface{} { // "" = leave, Yes/No = set
		switch strField(d, "no_manual_posting") {
		case "Yes":
			return true
		case "No":
			return false
		}
		return nil
	}()
	switch strField(d, "action") {
	case "Add":
		_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.gl_accounts (account_code, account_name, account_type, parent_code, no_manual_posting)
			VALUES ($1, $2, $3, NULLIF($4, ''), COALESCE($5::boolean, FALSE)) ON CONFLICT (account_code) DO NOTHING`, schema), code, name, typ, parent, flagArg)
	case "Change":
		_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.gl_accounts SET
				account_name = COALESCE(NULLIF($2, ''), account_name),
				account_type = COALESCE(NULLIF($3, ''), account_type),
				parent_code = CASE WHEN $6 = 'Yes' THEN NULL WHEN $4 <> '' THEN $4 ELSE parent_code END,
				no_manual_posting = COALESCE($5::boolean, no_manual_posting)
			WHERE account_code = $1`, schema), code, name, typ, parent, flagArg, strField(d, "clear_parent"))
	case "Block", "Unblock":
		_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.gl_accounts SET blocked = $2 WHERE account_code = $1`, schema), code, strField(d, "action") == "Block")
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('applied_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, schema), docID); err != nil {
		return err
	}
	return tx.Commit()
}

func init() {
	RegisterApprovalDecisionHook(GLAccountChangeDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := applyGLAccountChange(tenantID, docID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "gl_account_change", fmt.Sprintf("approved change %s not applied: %v", docID, err), "")
		}
	})
}
