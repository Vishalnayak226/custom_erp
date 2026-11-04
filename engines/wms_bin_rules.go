package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Stage 146.3.1 (D5) - one item per bin, and home bins per SKU.
//
// Bin mix rule: set per Zone (Zone.bin_mix_rule) with a Bin override
// (Bin.bin_mix_rule). Blank reads as Mixed, so nothing changes for a client
// that never sets one. enforceBinMixRule is the one check; PutawayToBin,
// MoveBinStock, RecordBatchPutaway and the VAS output putaway all run it.
//
// Home bins (HomeBin master): several bins per SKU in priority order, each
// with "allow other items". They are the first put-away suggestion; a home
// bin with allow_other_items = No is kept for its own SKU(s).

const (
	BinMixMixed      = "Mixed"
	BinMixSingleItem = "Single item"
	BinMixSingleLot  = "Single item single lot"
)

// effectiveBinMixRule returns the bin's rule: its own, else its zone's,
// else Mixed. An unknown bin reads as Mixed (the callers already refuse it).
func effectiveBinMixRule(tx *sql.Tx, schema, binCode string) (string, error) {
	var rule string
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(b.data->>'bin_mix_rule', ''), NULLIF(z.data->>'bin_mix_rule', ''), 'Mixed')
		FROM %[1]s.documents b
		LEFT JOIN %[1]s.documents z ON z.doctype = 'Zone' AND z.deleted_at IS NULL AND z.data->>'code' = NULLIF(b.data->>'zone', '')
		WHERE b.doctype = 'Bin' AND b.deleted_at IS NULL AND b.data->>'bin_code' = $1
		LIMIT 1`, schema), binCode).Scan(&rule)
	if err == sql.ErrNoRows {
		return BinMixMixed, nil
	}
	return rule, err
}

// enforceBinMixRule refuses stock of sku (and lot batchNo, when known) into
// binCode when the bin's mix rule or a home-bin reservation forbids it. The
// message names both items. Read-only; callers hold their own transaction.
func enforceBinMixRule(tx *sql.Tx, schema, binCode, sku, batchNo string) error {
	if err := enforceReceiptsBlock(tx, schema, binCode); err != nil { // 146.3.14
		return err
	}
	rule, err := effectiveBinMixRule(tx, schema, binCode)
	if err != nil {
		return err
	}
	if rule == BinMixSingleItem || rule == BinMixSingleLot {
		var held string
		err := tx.QueryRow(fmt.Sprintf(
			`SELECT sku FROM %s.bin_stock WHERE bin_code = $1 AND sku <> $2 AND qty > 0 ORDER BY sku LIMIT 1`, schema),
			binCode, sku).Scan(&held)
		if err == nil {
			return putawayRefused("bin %s is a single-item bin and holds %s - %s cannot go in it", binCode, held, sku)
		} else if err != sql.ErrNoRows {
			return err
		}
	}
	if rule == BinMixSingleLot && strings.TrimSpace(batchNo) != "" {
		var heldLot string
		err := tx.QueryRow(fmt.Sprintf(
			`SELECT batch_no FROM %s.bin_stock_batch WHERE bin_code = $1 AND sku = $2 AND batch_no <> $3 AND qty > 0 ORDER BY batch_no LIMIT 1`, schema),
			binCode, sku, batchNo).Scan(&heldLot)
		if err == nil {
			return putawayRefused("bin %s is a single-lot bin and holds lot %s of %s - lot %s cannot go in it", binCode, heldLot, sku, batchNo)
		} else if err != sql.ErrNoRows {
			return err
		}
	}
	// A home bin that does not allow other items is kept for its own SKU(s).
	var owner string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT h.data->>'sku' FROM %[1]s.documents h
		WHERE h.doctype = 'HomeBin' AND h.status = 'Active' AND h.deleted_at IS NULL
		  AND h.data->>'bin_code' = $1 AND COALESCE(h.data->>'allow_other_items', 'Yes') = 'No' AND h.data->>'sku' <> $2
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents o WHERE o.doctype = 'HomeBin' AND o.status = 'Active' AND o.deleted_at IS NULL
		                  AND o.data->>'bin_code' = $1 AND o.data->>'sku' = $2)
		ORDER BY h.data->>'sku' LIMIT 1`, schema), binCode, sku).Scan(&owner)
	if err == nil {
		return putawayRefused("bin %s is the home bin of %s and is kept for it - %s cannot go in it", binCode, owner, sku)
	} else if err != sql.ErrNoRows {
		return err
	}
	return nil
}

// HomeBin is one home-bin row for a SKU, with whether it can take stock now.
type HomeBin struct {
	BinCode     string `json:"bin_code"`
	Priority    int    `json:"priority"`
	AllowOthers bool   `json:"allow_other_items"`
	Usable      bool   `json:"usable"`
	Reason      string `json:"reason,omitempty"` // why it cannot take stock now
}

// HomeBinsForSKU lists the SKU's Active home bins at locationCode (blank =
// anywhere) in priority order, each with a usable flag and the reason when
// it is not. qty 1 is assumed for the capacity check.
func HomeBinsForSKU(tenantID, sku, locationCode string) ([]HomeBin, error) {
	return homeBinsFor(tenantID, sku, locationCode, 1, "")
}

func homeBinsFor(tenantID, sku, locationCode string, qty int, batchNo string) ([]HomeBin, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT h.data->>'bin_code', COALESCE(NULLIF(h.data->>'priority', '')::numeric, 999999)::int,
		       COALESCE(h.data->>'allow_other_items', 'Yes'),
		       COALESCE(b.data->>'location', ''), COALESCE(b.status, ''), COALESCE(b.data->>'bin_status', ''),
		       COALESCE(NULLIF(b.data->>'capacity', '')::numeric, 0),
		       COALESCE(NULLIF(b.data->>'max_weight', '')::numeric, 0),
		       COALESCE(NULLIF(b.data->>'max_volume', '')::numeric, 0),
		       COALESCE(b.data->>'zone', ''), b.id IS NOT NULL
		FROM %[1]s.documents h
		LEFT JOIN %[1]s.documents b ON b.doctype = 'Bin' AND b.deleted_at IS NULL AND b.data->>'bin_code' = h.data->>'bin_code'
		WHERE h.doctype = 'HomeBin' AND h.status = 'Active' AND h.deleted_at IS NULL AND h.data->>'sku' = $1
		ORDER BY 2, 1`, schema), sku)
	if err != nil {
		return nil, err
	}
	type raw struct {
		HomeBin
		loc, status, opState, zone   string
		maxQty, maxWeight, maxVolume float64
		exists                       bool
	}
	var raws []raw
	for rows.Next() {
		var r raw
		var allow string
		if err := rows.Scan(&r.BinCode, &r.Priority, &allow, &r.loc, &r.status, &r.opState, &r.maxQty, &r.maxWeight, &r.maxVolume, &r.zone, &r.exists); err != nil {
			rows.Close()
			return nil, err
		}
		r.AllowOthers = allow != "No"
		raws = append(raws, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(raws) == 0 {
		return nil, nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	var out []HomeBin
	for _, r := range raws {
		if r.exists && locationCode != "" && r.loc != "" && r.loc != locationCode {
			continue // a home bin at another location is not this receipt's
		}
		switch {
		case !r.exists:
			r.Reason = "bin not found"
		case r.status != "Active":
			r.Reason = "not Active"
		case r.opState == "Blocked" || r.opState == "Full" || r.opState == "Counting":
			r.Reason = r.opState
		default:
			if err := checkHazmatCompatibility(tx, schema, sku, r.zone, r.BinCode); err != nil {
				r.Reason = "hazmat zone"
			} else if err := enforceBinCapacity(tx, schema, r.BinCode, sku, qty, r.maxQty, r.maxWeight, r.maxVolume); err != nil {
				r.Reason = "no room"
			} else if err := enforceBinMixRule(tx, schema, r.BinCode, sku, batchNo); err != nil {
				r.Reason = "mix rule"
				if ve, ok := err.(*ValidationError); ok && ve.Code == "WMS-1313" {
					r.Reason = "closed to receipts"
				}
			}
		}
		r.Usable = r.Reason == ""
		out = append(out, r.HomeBin)
	}
	return out, nil
}

// suggestHomeBin is the first put-away suggestion: the first usable home bin
// of sku at locationCode. When there are home bins and none can take the
// stock, bin is "" and note says why for each, so the caller can fall back
// to today's logic and show the reason. No home bins at all: ("", "", nil).
func suggestHomeBin(tenantID, sku, locationCode string, qty int, batchNo string) (bin, note string, err error) {
	list, err := homeBinsFor(tenantID, sku, locationCode, qty, batchNo)
	if err != nil || len(list) == 0 {
		return "", "", err
	}
	var skipped []string
	for _, h := range list {
		if h.Usable {
			note = fmt.Sprintf("home bin %s (priority %d)", h.BinCode, h.Priority)
			if len(skipped) > 0 {
				note += "; skipped " + strings.Join(skipped, ", ")
			}
			return h.BinCode, note, nil
		}
		skipped = append(skipped, fmt.Sprintf("%s (%s)", h.BinCode, h.Reason))
	}
	return "", "no home bin can take it - " + strings.Join(skipped, ", "), nil
}

// dropMixRefusedBins removes the directed-putaway candidates whose mix rule
// or home-bin reservation would refuse sku. With no rule set anywhere the
// check passes every bin, so a client that never sets one sees the same list.
func dropMixRefusedBins(schema, sku, batchNo string, in []putawayBinCandidate) []putawayBinCandidate {
	if len(in) == 0 {
		return in
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return in
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return in
	}
	out := in[:0:0]
	for _, c := range in {
		if enforceBinMixRule(tx, schema, c.BinCode, sku, batchNo) == nil {
			out = append(out, c)
		}
	}
	return out
}
