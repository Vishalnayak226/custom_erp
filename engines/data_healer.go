package engines

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Data Healer (ask 207). A Heal is a one-time fix a client runs after a
// release that changed what existing records should hold - item 201's item
// alias is the first. A release never changes client data at deploy
// (CONTRIBUTING Article 5), so the heal waits in Settings > Data Healer: a
// banner on every screen says so, and the pages it covers (Heal.Doctypes)
// refuse to save until it has run. Run heal fills only what is still
// missing, a value typed on the screen where one was given, the proposed
// one otherwise, then records the heal Done (a DataHeal record). Done is
// final: like pasting a formula's values, it never runs again. A heal with
// nothing to fix in a client is recorded Done the first time it is checked,
// so a client set up after the release never sees it.
//
// A new heal is one healDef added to dataHeals.

const (
	dataHealDoctype   = "DataHeal"
	HealStatusWaiting = "Waiting"
	HealStatusDone    = "Done"
	// healRecordsLimit is the most records the screen lists to review; Run
	// heal fills the rest with their proposed values.
	healRecordsLimit = 500
)

// HealRecord is one record a heal fills: what it holds now and what it gets.
type HealRecord struct {
	Doctype  string `json:"doctype"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	Current  string `json:"current"`
	Proposed string `json:"proposed"`
}

// HealState is a heal as the screen and the banner show it.
type HealState struct {
	ID         string   `json:"id"`
	Item       string   `json:"item"`
	Title      string   `json:"title"`
	FieldLabel string   `json:"field_label"`
	Doctypes   []string `json:"doctypes"`
	MaxLen     int      `json:"max_len,omitempty"`
	Status     string   `json:"status"`
	Count      int      `json:"count"`
	DoneAt     string   `json:"done_at,omitempty"`
	DoneBy     string   `json:"done_by,omitempty"`
	Fixed      int      `json:"fixed"`
	Typed      int      `json:"typed"`
}

// HealRunResult says what Run heal changed.
type HealRunResult struct {
	Fixed       int  `json:"fixed"`
	Typed       int  `json:"typed"`
	AlreadyDone bool `json:"already_done"`
}

type healQuerier interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
}

type healDef struct {
	ID, Item, Title, FieldLabel string
	Doctypes                    []string
	MaxLen                      int
	// count is how many records still need the heal.
	count func(q healQuerier, schema string) (int, error)
	// records lists up to limit of them with the value each would get.
	records func(q healQuerier, schema string, limit int) ([]HealRecord, error)
	// check refuses a typed value before anything is written.
	check func(r HealRecord, typed string) error
	// apply fills every record still missing the value - typed["Doctype/id"]
	// where given - and returns how many it filled and how many of those took
	// a typed value. Runs inside one transaction holding the heal's lock.
	apply func(q healQuerier, schema string, typed map[string]string) (fixed, typedUsed int, err error)
}

func dataHeals() []healDef {
	return []healDef{itemAliasHeal(), printerNameHeal()}
}

func findHeal(id string) (healDef, bool) {
	for _, h := range dataHeals() {
		if h.ID == id {
			return h, true
		}
	}
	return healDef{}, false
}

// dataHealDocID is the DataHeal record of one heal in one client. The tenant
// is part of the hash because document ids are unique across clients.
func dataHealDocID(tenantID, healID string) string {
	sum := sha256.Sum256([]byte(tenantID + "|" + healID))
	return "HEAL-" + strings.ToUpper(hex.EncodeToString(sum[:8]))
}

func (h healDef) state() HealState {
	return HealState{ID: h.ID, Item: h.Item, Title: h.Title, FieldLabel: h.FieldLabel, Doctypes: h.Doctypes, MaxLen: h.MaxLen}
}

// readHealDone fills st from the heal's DataHeal record; false when it has none.
func readHealDone(q healQuerier, schema, docID string, st *HealState) (bool, error) {
	var raw string
	err := q.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`, schema),
		docID, dataHealDoctype).Scan(&raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &d)
	st.Status = HealStatusDone
	st.DoneAt, st.DoneBy = strField(d, "done_at"), strField(d, "done_by")
	st.Fixed, st.Typed = healIntField(d, "fixed"), healIntField(d, "typed")
	return true, nil
}

func healIntField(d map[string]interface{}, k string) int {
	if f, ok := d[k].(float64); ok {
		return int(f)
	}
	return 0
}

func recordHealDone(q healQuerier, schema, tenantID string, h healDef, actor string, fixed, typed int) error {
	data, _ := json.Marshal(map[string]interface{}{
		"heal_id": h.ID, "title": h.Title, "status": HealStatusDone,
		"done_at": time.Now().UTC().Format(time.RFC3339), "done_by": actor, "fixed": fixed, "typed": typed,
	})
	_, err := q.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, schema), dataHealDocID(tenantID, h.ID), dataHealDoctype, data, HealStatusDone, actor)
	return err
}

// ListHeals is every heal with its state in this client. One with nothing
// left to fix and no record yet is recorded Done here (by "system").
func ListHeals(tenantID string) ([]HealState, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []HealState{}
	for _, h := range dataHeals() {
		st, err := healStateOf(schema, tenantID, h)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func healStateOf(schema, tenantID string, h healDef) (HealState, error) {
	st := h.state()
	if done, err := readHealDone(db.DB, schema, dataHealDocID(tenantID, h.ID), &st); err != nil || done {
		return st, err
	}
	n, err := h.count(db.DB, schema)
	if err != nil {
		return st, err
	}
	if n == 0 {
		if err := recordHealDone(db.DB, schema, tenantID, h, "system", 0, 0); err != nil {
			return st, err
		}
		_, err := readHealDone(db.DB, schema, dataHealDocID(tenantID, h.ID), &st)
		return st, err
	}
	st.Status, st.Count = HealStatusWaiting, n
	return st, nil
}

// HealRecords is one heal's state and, while it waits, the records it would
// fill (at most healRecordsLimit; Count says how many there are).
func HealRecords(tenantID, healID string) (HealState, []HealRecord, error) {
	h, ok := findHeal(healID)
	if !ok {
		return HealState{}, nil, healNotFound()
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return HealState{}, nil, err
	}
	st, err := healStateOf(schema, tenantID, h)
	if err != nil || st.Status == HealStatusDone {
		return st, []HealRecord{}, err
	}
	recs, err := h.records(db.DB, schema, healRecordsLimit)
	if recs == nil {
		recs = []HealRecord{}
	}
	return st, recs, err
}

func healNotFound() error {
	return &ValidationError{Code: "MASTER-1703", Message: "That heal was not found."}
}

// RunHeal runs one heal for this client. typed maps "Doctype/id" to a value
// typed on the screen; every typed value is checked before anything is
// written. All of it is one transaction under the heal's lock, so two
// admins pressing Run together heal once. A heal already Done changes
// nothing and says so.
func RunHeal(tenantID, healID, actor string, typed map[string]string) (HealRunResult, error) {
	var res HealRunResult
	h, ok := findHeal(healID)
	if !ok {
		return res, healNotFound()
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	clean := map[string]string{}
	for k, v := range typed {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		parts := strings.SplitN(k, "/", 2)
		if len(parts) != 2 || parts[1] == "" || !containsString(h.Doctypes, parts[0]) {
			continue
		}
		if err := h.check(HealRecord{Doctype: parts[0], ID: parts[1]}, v); err != nil {
			return res, err
		}
		clean[k] = v
	}
	docID := dataHealDocID(tenantID, h.ID)
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, docID); err != nil {
		return res, err
	}
	var st HealState
	if done, err := readHealDone(tx, schema, docID, &st); err != nil {
		return res, err
	} else if done {
		res.AlreadyDone = true
		return res, nil
	}
	fixed, typedUsed, err := h.apply(tx, schema, clean)
	if err != nil {
		return res, err
	}
	// A typed value for a record no longer in the heal is not counted.
	res.Fixed, res.Typed = fixed, typedUsed
	if err := recordHealDone(tx, schema, tenantID, h, actor, res.Fixed, res.Typed); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	LogAuditEvent(tenantID, actor, "DATA_HEAL_RUN", "Success", fmt.Sprintf("heal=%s fixed=%d typed=%d", h.ID, res.Fixed, res.Typed))
	return res, nil
}

// validateDataHealRules keeps DataHeal records to Run heal: the generic
// screen and API cannot create or change one.
func validateDataHealRules() error {
	return &ValidationError{Code: "MASTER-1704", Message: "Heals are recorded by Run heal only."}
}
