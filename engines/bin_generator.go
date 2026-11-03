package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Stage 146.3.4 - bin generator and the pick sequence.
//
// GenerateBins builds Aisle x Rack x Shelf x Bin from range specs ("A-C",
// "01-12", "A,B,F") and a code format ("{aisle}-{rack}-{shelf}-{bin}"), as
// ordinary Bin masters. Idempotent: a bin code already in use is counted as
// existing and left alone, so re-running (or widening a range) creates only
// what is new. Preview returns the rows without writing anything.

const (
	binGenMaxPerRange = 2000
	binGenMaxTotal    = 5000
	binGenDefaultFmt  = "{aisle}-{rack}-{shelf}-{bin}"
	// noPickSeq is the pick sequence of a bin that has none: it walks after
	// every sequenced bin and in today's zone / aisle / rack / bin order.
	noPickSeq = 999999
)

// BinGenInput is one generator run.
type BinGenInput struct {
	Location string `json:"location"`
	Zone     string `json:"zone"`
	Format   string `json:"format"`
	Aisles   string `json:"aisles"`
	Racks    string `json:"racks"`
	Shelves  string `json:"shelves"`
	Bins     string `json:"bins"`
	Preview  bool   `json:"preview"`
	UserID   string `json:"-"`
	// Scope is the calling session. When set, the location is held to it the way a
	// hand-made Bin is (49.3.8a). nil is an engine-internal caller.
	Scope *SessionScope `json:"-"`
}

// BinGenRow is one bin the run covers.
type BinGenRow struct {
	Code     string `json:"code"`
	Aisle    string `json:"aisle"`
	Rack     string `json:"rack"`
	Level    string `json:"level"`
	Position string `json:"position"`
	Exists   bool   `json:"exists"`
}

// BinGenResult is the preview or the outcome.
type BinGenResult struct {
	Rows     []BinGenRow `json:"rows"`
	Total    int         `json:"total"`
	Created  int         `json:"created"`
	Existing int         `json:"existing"`
}

var binRangeNumeric = regexp.MustCompile(`^\d+$`)
var binRangeLetter = regexp.MustCompile(`^[A-Za-z]$`)

// expandBinRange turns "A-C", "01-12", "A,C" or a mix into its values. A
// dash item that is neither a number pair nor a letter pair is kept as is,
// so a code such as "R2-R2" is never silently rewritten.
func expandBinRange(spec string) ([]string, error) {
	var out []string
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, "-")
		if len(parts) == 2 {
			a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			switch {
			case binRangeNumeric.MatchString(a) && binRangeNumeric.MatchString(b):
				from, _ := strconv.Atoi(a)
				to, _ := strconv.Atoi(b)
				if to-from+1 > binGenMaxPerRange {
					return nil, fmt.Errorf("range %q is too large (at most %d values)", item, binGenMaxPerRange)
				}
				width := 0
				if len(a) > 1 && a[0] == '0' {
					width = len(a)
				}
				for n := from; n <= to; n++ {
					out = append(out, fmt.Sprintf("%0*d", width, n))
				}
				continue
			case binRangeLetter.MatchString(a) && binRangeLetter.MatchString(b):
				for c := a[0]; c <= b[0]; c++ {
					out = append(out, string(rune(c)))
				}
				continue
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// GenerateBins previews or creates the bins of in. See the file comment.
func GenerateBins(tenantID string, in BinGenInput) (*BinGenResult, error) {
	in.Location, in.Zone = strings.TrimSpace(in.Location), strings.TrimSpace(in.Zone)
	if in.Location == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Location", Message: "pick the location the bins are for"}
	}
	format := strings.TrimSpace(in.Format)
	if format == "" {
		format = binGenDefaultFmt
	}
	dims := [4][]string{}
	for i, spec := range []string{in.Aisles, in.Racks, in.Shelves, in.Bins} {
		v, err := expandBinRange(spec)
		if err != nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: err.Error()}
		}
		if len(v) == 0 {
			v = []string{""}
		}
		dims[i] = v
	}
	total := len(dims[0]) * len(dims[1]) * len(dims[2]) * len(dims[3])
	if total > binGenMaxTotal {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("%d bins in one run is too many (at most %d)", total, binGenMaxTotal)}
	}
	if len(dims[0][0])+len(dims[1][0])+len(dims[2][0])+len(dims[3][0]) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0001", Message: "give at least one of aisle, rack, shelf or bin"}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1))`, schema), in.Location).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, &ValidationError{Code: "GLOBAL-0004", SubFor: "Location", Message: fmt.Sprintf("location %q not found", in.Location)}
	}
	if in.Scope != nil {
		if named := LocationOutOfScope("Bin", *in.Scope, map[string]interface{}{"location": in.Location}); named != "" {
			return nil, &ValidationError{Code: "GLOBAL-0011", Message: LocationOutOfScopeMessage(named, in.Scope.LocationCode)}
		}
	}
	if in.Zone != "" {
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Zone' AND data->>'code' = $1 AND status = 'Active' AND deleted_at IS NULL)`, schema), in.Zone).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, &ValidationError{Code: "META-0198", SubFor: "Zone", Message: fmt.Sprintf("no Active zone with code %q exists - create it on the Zone master first", in.Zone)}
		}
	}

	res := &BinGenResult{Rows: make([]BinGenRow, 0, total)}
	for _, a := range dims[0] {
		for _, r := range dims[1] {
			for _, s := range dims[2] {
				for _, b := range dims[3] {
					code := binGenCode(format, a, r, s, b)
					if code == "" {
						return nil, &ValidationError{Code: "GLOBAL-0002", Message: "the code format gives an empty bin code"}
					}
					res.Rows = append(res.Rows, BinGenRow{Code: code, Aisle: a, Rack: r, Level: s, Position: b})
				}
			}
		}
	}
	res.Total = len(res.Rows)
	seen := map[string]bool{}
	for _, row := range res.Rows {
		if seen[row.Code] {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("the code format gives %q twice - include every ranged part in it", row.Code)}
		}
		seen[row.Code] = true
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	for i := range res.Rows {
		row := &res.Rows[i]
		if err := tx.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1 AND deleted_at IS NULL)`, schema), row.Code).Scan(&row.Exists); err != nil {
			return nil, err
		}
		if row.Exists {
			res.Existing++
			continue
		}
		if in.Preview {
			continue
		}
		data := map[string]interface{}{"bin_code": row.Code, "location": in.Location, "status": "Active"}
		for k, v := range map[string]string{"zone": in.Zone, "aisle": row.Aisle, "rack": row.Rack, "level": row.Level, "position": row.Position} {
			if v != "" {
				data[k] = v
			}
		}
		// The same checks and numbering a hand-made Bin gets.
		if err := ValidateDocument(tenantID, "Bin", data); err != nil {
			return nil, err
		}
		if err := ValidateMasterDataRules(tenantID, "", "Bin", data); err != nil {
			return nil, err
		}
		if err := PrepareMasterNumber(tenantID, in.Location, "Bin", true, data); err != nil {
			return nil, err
		}
		id, _ := data["id"].(string)
		if id == "" {
			id = "BIN-" + row.Code
		}
		raw, _ := json.Marshal(data)
		n, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', $3) ON CONFLICT (id) DO NOTHING`, schema),
			id, raw, binGenUser(in.UserID))
		if err != nil {
			return nil, err
		}
		if c, _ := n.RowsAffected(); c == 0 {
			row.Exists = true
			res.Existing++
			continue
		}
		res.Created++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if !in.Preview && res.Created > 0 {
		LogAuditEvent(tenantID, in.UserID, "BIN_GENERATE", "SUCCESS", fmt.Sprintf("Generated %d bins at %s (%d already existed)", res.Created, in.Location, res.Existing))
	}
	return res, nil
}

// binGenCode fills the format and tidies the dashes an empty part leaves.
func binGenCode(format, aisle, rack, shelf, bin string) string {
	code := strings.NewReplacer("{aisle}", aisle, "{rack}", rack, "{shelf}", shelf, "{bin}", bin).Replace(format)
	for strings.Contains(code, "--") {
		code = strings.ReplaceAll(code, "--", "-")
	}
	return strings.Trim(strings.TrimSpace(code), "-")
}

func binGenUser(v string) string {
	if strings.TrimSpace(v) == "" {
		return "system"
	}
	return v
}

// binWalk is the sort key of a bin on a pick list.
type binWalk struct {
	Seq                    int
	Zone, Aisle, Rack, Bin string
}

// walkBefore orders two bins for a pick list: a lower pick sequence first,
// then today's zone / aisle / rack / bin order. With no sequence set anywhere
// every Seq is noPickSeq, so the order is exactly what it was.
func walkBefore(a, b binWalk) bool {
	if a.Seq != b.Seq {
		return a.Seq < b.Seq
	}
	if a.Zone != b.Zone {
		return a.Zone < b.Zone
	}
	if a.Aisle != b.Aisle {
		return a.Aisle < b.Aisle
	}
	if a.Rack != b.Rack {
		return a.Rack < b.Rack
	}
	return a.Bin < b.Bin
}
