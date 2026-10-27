package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"strings"
)

// Stage 49.13.5: restore is a security control, so a restore drill checks
// more than "rows came back". VerifyRestoredDatabase runs against whatever
// database DATABASE_URL names - the drill's throwaway copy, never live - and
// answers, for every client in it:
//
//   - Ledger balances: every posted debit has its credit (gl_postings).
//   - Audit evidence verifies with this server's signing key - which also
//     proves the key needed to trust the restored history is available.
//   - The reconciliation checks (control totals, goods with parties, stock
//     pools, print evidence) - reported, not blocking: the same differences
//     would show on live's own Reconciliation screen.
//
// And once for the database: which migrations this binary would still apply
// (a backup older than the release is normal; run -migrate before serving).
//
// The checks write only to the restored copy (reconciliation_results rows).

// RestoreCheck is one check's result. Blocking failures mean the restored
// copy cannot be trusted as it is.
type RestoreCheck struct {
	Tenant   string `json:"tenant"`
	Check    string `json:"check"`
	OK       bool   `json:"ok"`
	Blocking bool   `json:"blocking"`
	Detail   string `json:"detail"`
}

// VerifyRestoredDatabase runs every check; the error is for a database it
// could not read at all.
func VerifyRestoredDatabase() ([]RestoreCheck, error) {
	var out []RestoreCheck
	pending, err := db.PendingMigrations()
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	mig := RestoreCheck{Tenant: "(database)", Check: "migrations", OK: true, Detail: "the schema matches this release"}
	if len(pending) > 0 {
		mig.Detail = fmt.Sprintf("%d migration(s) newer than the backup - run -migrate before serving this copy", len(pending))
	}
	out = append(out, mig)

	rows, err := db.DB.Query(`SELECT MIN(tenant_id), schema_name FROM public.tenants GROUP BY schema_name ORDER BY schema_name`)
	if err != nil {
		return nil, fmt.Errorf("read clients: %w", err)
	}
	type client struct{ id, schema string }
	var clients []client
	for rows.Next() {
		var c client
		if err := rows.Scan(&c.id, &c.schema); err != nil {
			rows.Close()
			return nil, err
		}
		clients = append(clients, c)
	}
	rows.Close()

	for _, c := range clients {
		var debit, credit float64
		ledger := RestoreCheck{Tenant: c.id, Check: "ledger balances", Blocking: true}
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0) FROM %s.gl_postings`, c.schema)).Scan(&debit, &credit); err != nil {
			ledger.Detail = "could not read: " + err.Error()
		} else {
			ledger.OK = math.Abs(debit-credit) < 0.01
			ledger.Detail = fmt.Sprintf("debits %.2f, credits %.2f", debit, credit)
		}
		out = append(out, ledger)

		audit := RestoreCheck{Tenant: c.id, Check: "audit evidence", Blocking: true}
		if v, err := VerifyAuditEvidence(c.id); err != nil {
			audit.Detail = "could not verify: " + err.Error()
		} else {
			audit.OK = v.Intact
			audit.Detail = fmt.Sprintf("%d signed row(s) verified, %d checkpoint(s)", v.Verified, v.CheckpointsOK)
			if !v.Intact {
				audit.Detail = fmt.Sprintf("%d row(s) and %d checkpoint(s) do not verify - tampered, or this server's audit signing key is not the one that wrote them",
					len(v.TamperedRows), len(v.CheckpointsBad))
				if len(v.MissingKeys) > 0 {
					// BLD-053: say which key the restore host is missing.
					audit.Detail += fmt.Sprintf("; %d row(s) are signed by key(s) %s, which this server does not hold - load them and verify again",
						v.UnverifiableRows, strings.Join(v.MissingKeys, ", "))
				}
			}
		}
		out = append(out, audit)

		recon := RestoreCheck{Tenant: c.id, Check: "reconciliations"}
		if results, err := RunReconciliationChecks(c.id, true); err != nil {
			recon.Detail = "could not run: " + err.Error()
		} else {
			var untied []string
			for _, r := range results {
				if !r.Tied {
					untied = append(untied, r.Label)
				}
			}
			recon.OK = len(untied) == 0
			recon.Detail = fmt.Sprintf("%d check(s) tied", len(results)-len(untied))
			if len(untied) > 0 {
				recon.Detail = "not tied (compare with live's Reconciliation screen): " + strings.Join(untied, "; ")
			}
		}
		out = append(out, recon)
	}
	return out, nil
}
