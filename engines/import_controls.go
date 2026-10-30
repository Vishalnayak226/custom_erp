package engines

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ImportRulesVersion (Stage 47.13.3) names the validation and transformation
// rules an import ran under; every ImportJob records it. Bump it whenever
// those rules change (ValidateDocument's import checks, header mapping,
// number / date parsing, the control-total rules), so an old job can be
// told from one run under today's rules.
const ImportRulesVersion = "2026.10.29"

// ImportSource is an import's source lineage: the file as uploaded - its
// name, size and SHA-256 - and who uploaded it, when. Stored on the
// ImportJob so a posted batch can be traced back to the exact file.
type ImportSource struct {
	FileName   string `json:"file_name,omitempty"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
	UploadedBy string `json:"uploaded_by"`
	UploadedAt string `json:"uploaded_at"`
}

// importSourceReader hashes and counts the bytes an import reads, so the
// lineage is of the file exactly as parsed, with no second pass over it.
// A reader that knows its name (an *os.File, or NamedUpload) names the file.
type importSourceReader struct {
	r    io.Reader
	h    hash.Hash
	size int64
	name string
}

func newImportSourceReader(r io.Reader) *importSourceReader {
	s := &importSourceReader{r: r, h: sha256.New()}
	if n, ok := r.(interface{ Name() string }); ok {
		s.name = filepath.Base(strings.TrimSpace(n.Name()))
		if s.name == "." || s.name == string(filepath.Separator) {
			s.name = ""
		}
	}
	return s
}

type namedReader struct {
	io.Reader
	name string
}

func (n namedReader) Name() string { return n.name }

// NamedUpload gives an upload stream its file name, so the import it feeds
// records that name in its lineage - the handler that received the upload
// is the one that knows it.
func NamedUpload(r io.Reader, fileName string) io.Reader {
	return namedReader{Reader: r, name: fileName}
}

func (s *importSourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.h.Write(p[:n])
		s.size += int64(n)
	}
	return n, err
}

// stampImportSource fills the result's rules version and lineage from the
// reader the import parsed.
func stampImportSource(res *ImportResult, src *importSourceReader, uploadedBy string) {
	if res == nil || src == nil {
		return
	}
	res.RulesVersion = ImportRulesVersion
	res.Source = &ImportSource{
		FileName: src.name, SizeBytes: src.size, SHA256: hex.EncodeToString(src.h.Sum(nil)),
		UploadedBy: uploadedBy, UploadedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// Stage 47.13.3: batch control totals. Every import - a plain CSV, a
// template import, a resumed job, a preview - reports what the file held
// against what was posted: rows read, accepted and rejected, how many were
// new or updates (an all-update run is a rerun of a file already in), and
// the sum of each Number column over the file and over the accepted rows.
// The row counts must balance; if they ever do not, the result says so
// instead of leaving a partial import silent.

// ImportControlTotals is the control block on an ImportResult.
type ImportControlTotals struct {
	RowsRead     int                 `json:"rows_read"`
	RowsAccepted int                 `json:"rows_accepted"`
	RowsRejected int                 `json:"rows_rejected"`
	Created      int                 `json:"created"`
	Updated      int                 `json:"updated"`
	Rerun        bool                `json:"rerun"`
	Balanced     bool                `json:"balanced"`
	Columns      []ImportColumnTotal `json:"columns,omitempty"`
}

// ImportColumnTotal is one Number column's sum: in the file, accepted and
// rejected. Only cells holding a plain number count.
type ImportColumnTotal struct {
	Field    string  `json:"field"`
	Label    string  `json:"label"`
	Read     float64 `json:"read"`
	Accepted float64 `json:"accepted"`
	Rejected float64 `json:"rejected"`
}

// importControls accumulates the totals during one import. rowVals is
// parallel to the import's docRows and is captured before validation, so an
// accepted row adds exactly what the file said, not a default filled in later.
type importControls struct {
	columns []ImportColumnTotal
	rowVals []map[int]float64 // row index -> column index -> value
}

func newImportControls(fields []FieldMeta, docRows []map[string]interface{}) *importControls {
	c := &importControls{rowVals: make([]map[int]float64, len(docRows))}
	for _, f := range fields {
		if f.Fieldtype != "Number" {
			continue
		}
		col := -1
		for i, row := range docRows {
			v, ok := importNumber(row[f.Fieldname])
			if !ok {
				continue
			}
			if col < 0 {
				col = len(c.columns)
				c.columns = append(c.columns, ImportColumnTotal{Field: f.Fieldname, Label: f.Label})
			}
			if c.rowVals[i] == nil {
				c.rowVals[i] = map[int]float64{}
			}
			c.rowVals[i][col] = v
			c.columns[col].Read += v
		}
	}
	return c
}

func importNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" || !isPlainNumber(s) {
		return 0, false
	}
	n, _ := strconv.ParseFloat(s, 64)
	return n, true
}

// accept adds one posted row's values; idx is its index in docRows.
func (c *importControls) accept(idx int) {
	if c == nil || idx < 0 || idx >= len(c.rowVals) {
		return
	}
	for col, v := range c.rowVals[idx] {
		c.columns[col].Accepted += v
	}
}

func roundControl(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// finishImportControls fills result.Controls from the finished counts. Row
// counts that do not balance add an error line rather than pass silently.
func finishImportControls(result *ImportResult) {
	t := &ImportControlTotals{
		RowsRead:     result.TotalRows,
		RowsAccepted: result.SuccessRows,
		RowsRejected: result.FailedRows,
		Created:      len(result.CreatedIDs),
		Updated:      len(result.UpdatedIDs),
	}
	t.Rerun = t.RowsAccepted > 0 && t.Created == 0
	t.Balanced = t.RowsRead == t.RowsAccepted+t.RowsRejected && t.RowsAccepted == t.Created+t.Updated
	if result.controls != nil {
		for _, col := range result.controls.columns {
			col.Read, col.Accepted = roundControl(col.Read), roundControl(col.Accepted)
			col.Rejected = roundControl(col.Read - col.Accepted)
			t.Columns = append(t.Columns, col)
		}
	}
	if !t.Balanced {
		result.Errors = append(result.Errors, RowValidationError{Message: fmt.Sprintf(
			"control totals do not balance: %d read, %d accepted (%d new, %d updated), %d rejected - check the import job before relying on it",
			t.RowsRead, t.RowsAccepted, t.Created, t.Updated, t.RowsRejected)})
	}
	result.Controls = t
}
