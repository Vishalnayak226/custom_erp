package main

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"custom_erp/engines"
)

// Reconciliation evidence. The ledger and stock checks are plain SQL; the
// subledger ties and the audit checks call the same engines the product uses
// (RunReconciliationChecks, VerifyAuditEvidence, WriteAuditCheckpoint), so the
// evidence is the product's own judgement, not a second opinion written here.

// StockExpect is a stock movement a journey promises.
type StockExpect struct {
	SKU, Loc string
	Delta    float64
	Why      string
}

func (r *Runner) onHand(sku, loc string) float64 {
	var v sql.NullFloat64
	_ = r.DB.QueryRow(fmt.Sprintf(`SELECT SUM(on_hand) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, r.Cfg.Schema), sku, loc).Scan(&v)
	return v.Float64
}

// StockBase remembers on-hand before a journey.
func (j *Journey) StockBase(sku, loc string) {
	if j.stockBase == nil {
		j.stockBase = map[string]float64{}
	}
	j.stockBase[sku+"|"+loc] = j.R.onHand(sku, loc)
}

// Expect declares a stock delta the journey must have produced.
func (j *Journey) Expect(sku, loc string, delta float64, why string) {
	j.expects = append(j.expects, StockExpect{SKU: sku, Loc: loc, Delta: delta, Why: why})
}

// InjectFault writes an unbalanced ledger row so the evidence must go red.
func (r *Runner) InjectFault() error {
	_, err := r.DB.Exec(fmt.Sprintf(`INSERT INTO %s.gl_postings (account_code, debit, credit, document_type, document_id) VALUES ('1200', 777, 0, 'JourneyLabInject', $1)`, r.Cfg.Schema), fmt.Sprintf("inject-%d", time.Now().UnixNano()))
	return err
}

// RemoveInjected deletes only the rows this tool injected.
func (r *Runner) RemoveInjected() {
	_, _ = r.DB.Exec(fmt.Sprintf(`DELETE FROM %s.gl_postings WHERE document_type = 'JourneyLabInject'`, r.Cfg.Schema))
}

// Reconcile gathers the evidence for a journey that has run its steps.
func (j *Journey) Reconcile() []ReconCheck {
	r := j.R
	var out []ReconCheck
	sch := r.Cfg.Schema

	var dr, cr int64
	if err := r.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM %s.gl_postings`, sch)).Scan(&dr, &cr); err != nil {
		out = append(out, ReconCheck{Kind: "gl", Name: "ledger total debits = credits", Detail: err.Error()})
	} else {
		out = append(out, ReconCheck{Kind: "gl", Name: "ledger total debits = credits", Pass: dr == cr, Diff: float64(dr - cr), Detail: fmt.Sprintf("debits %d, credits %d", dr, cr)})
	}
	var unbal int
	_ = r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s.gl_postings GROUP BY document_type, document_id HAVING SUM(debit) <> SUM(credit)) x`, sch)).Scan(&unbal)
	out = append(out, ReconCheck{Kind: "money", Name: "every voucher balances (debits = credits per document)", Pass: unbal == 0, Diff: float64(unbal), Detail: fmt.Sprintf("%d unbalanced voucher(s)", unbal)})

	var neg int
	_ = r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.inventory_availability WHERE on_hand < 0`, sch)).Scan(&neg)
	out = append(out, ReconCheck{Kind: "stock", Name: "no negative on-hand", Pass: neg == 0, Diff: float64(neg), Detail: fmt.Sprintf("%d row(s) below zero", neg)})
	for _, e := range j.expects {
		got := r.onHand(e.SKU, e.Loc) - j.stockBase[e.SKU+"|"+e.Loc]
		out = append(out, ReconCheck{Kind: "stock", Name: fmt.Sprintf("%s at %s moved by %+.0f (%s)", e.SKU, e.Loc, e.Delta, e.Why),
			Pass: math.Abs(got-e.Delta) < 0.0001, Diff: got - e.Delta, Detail: fmt.Sprintf("expected %+.0f, observed %+.0f", e.Delta, got)})
	}

	if r.Cfg.Quick {
		return out
	}
	res, err := engines.RunReconciliationChecks(r.Cfg.Tenant, true)
	if err != nil {
		out = append(out, ReconCheck{Kind: "subledger", Name: "engine reconciliation run", Detail: err.Error()})
	}
	for _, c := range res {
		kind := "subledger"
		if strings.Contains(c.CheckID, "stock") || strings.Contains(c.CheckID, "inventory") || strings.Contains(c.CheckID, "custody") {
			kind = "stock"
		}
		out = append(out, ReconCheck{Kind: kind, Name: "engine: " + c.CheckID, Pass: c.Tied, Diff: c.Difference, Detail: trunc(c.Detail, 160)})
	}

	out = append(out, j.auditChecks()...)
	return out
}

func (j *Journey) auditChecks() []ReconCheck {
	r := j.R
	var out []ReconCheck
	cp, err := engines.WriteAuditCheckpoint(r.Cfg.Tenant, "journeylab")
	switch {
	case err != nil:
		out = append(out, ReconCheck{Kind: "audit", Name: "audit checkpoint sealed", Detail: err.Error()})
	case cp == nil:
		out = append(out, ReconCheck{Kind: "audit", Name: "audit checkpoint sealed", Pass: true, Detail: "nothing new to seal since the last checkpoint"})
	default:
		out = append(out, ReconCheck{Kind: "audit", Name: "audit checkpoint sealed", Pass: true, Detail: fmt.Sprintf("rows %d-%d sealed", cp.FromSeq, cp.ToSeq)})
	}
	v, err := engines.VerifyAuditEvidence(r.Cfg.Tenant)
	if err != nil {
		return append(out, ReconCheck{Kind: "audit", Name: "audit evidence verifies", Detail: err.Error()})
	}
	out = append(out, ReconCheck{Kind: "audit", Name: "audit evidence verifies (no tampered row, no bad checkpoint)", Pass: v.Intact && len(v.TamperedRows) == 0 && len(v.CheckpointsBad) == 0,
		Detail: fmt.Sprintf("%d signed row(s) verified, %d unsigned legacy, %d unverifiable, %d checkpoint(s) verified", v.Verified, v.UnsignedLegacy, v.UnverifiableRows, v.CheckpointsOK)})
	out = append(out, ReconCheck{Kind: "audit", Name: "audit checkpoints present", Pass: v.CheckpointsOK > 0, Warn: v.CheckpointsOK == 0, Detail: fmt.Sprintf("%d verified checkpoint(s)", v.CheckpointsOK)})
	added, writes := int64(0), 0
	for _, s := range j.Rec.Steps {
		added += s.DB.AuditRows
		writes += s.HTTP.Writes
	}
	if writes > 0 {
		out = append(out, ReconCheck{Kind: "audit", Name: "journey left audit rows", Pass: added > 0, Detail: fmt.Sprintf("%d audit row(s) added during the journey", added)})
	}
	return out
}
