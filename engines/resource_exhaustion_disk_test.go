package engines

import (
	"bytes"
	"custom_erp/db"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// 49.10.5: a full or nearly full disk fails in words and leaves nothing
// half-written, for the two file writers outside the media store (which
// 49.13.7 already covers): the audit archive and the brand logo.

// shortWrite stands in a disk that has room for n bytes: it writes that much,
// then fails as a full disk does.
func shortWrite(n int) func(string, []byte, os.FileMode) error {
	return func(name string, data []byte, perm os.FileMode) error {
		if n > len(data) {
			n = len(data)
		}
		if err := os.WriteFile(name, data[:n], perm); err != nil {
			return err
		}
		return &os.PathError{Op: "write", Path: name, Err: syscall.ENOSPC}
	}
}

func TestAuditArchiveOnAFullDiskWritesAndDeletesNothing49105(t *testing.T) {
	old := writeArchiveFile
	t.Cleanup(func() { writeArchiveFile = old })
	writeArchiveFile = shortWrite(10)
	dir := t.TempDir()
	// The failure comes before any database step, so no connection is needed:
	// no audit row can have been deleted.
	id, err := writeArchiveWindow("t", "tenant_x", archiveCandidate{}, nil, dir)
	if err == nil || id != "" || !errors.Is(err, syscall.ENOSPC) ||
		!strings.Contains(err.Error(), "the server's disk is full") || strings.Contains(err.Error(), dir) {
		t.Fatalf("full disk: id=%q err=%v - want the plain 'disk is full' error, no path", id, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("a partial archive file was left behind: %v", left)
	}
	// Any other write error passes through, still without a leftover file.
	writeArchiveFile = func(name string, data []byte, perm os.FileMode) error {
		_ = os.WriteFile(name, data[:1], perm)
		return os.ErrPermission
	}
	if _, err := writeArchiveWindow("t", "tenant_x", archiveCandidate{}, nil, dir); err == nil || errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("other errors must pass through: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("a partial archive file was left behind: %v", left)
	}
}

func TestBrandLogoOnAFullDiskKeepsTheOldOne49105(t *testing.T) {
	db.InitDB(testConnStr())
	oldDir, oldWrite := mediaStoreDir, writeBrandFile
	mediaStoreDir = t.TempDir()
	t.Cleanup(func() { mediaStoreDir, writeBrandFile = oldDir, oldWrite })

	var logo bytes.Buffer
	_ = png.Encode(&logo, image.NewRGBA(image.Rect(0, 0, 120, 40)))
	if err := SaveBrandLogo("default", logo.Bytes()); err != nil {
		t.Fatalf("a logo is kept while the disk has room: %v", err)
	}
	path, _ := brandLogoPath("default")
	before, _ := os.ReadFile(path)

	var bigger bytes.Buffer
	_ = png.Encode(&bigger, image.NewRGBA(image.Rect(0, 0, 240, 80)))
	writeBrandFile = shortWrite(20)
	err := SaveBrandLogo("default", bigger.Bytes())
	if err == nil || !errors.Is(err, syscall.ENOSPC) || !strings.Contains(err.Error(), "the server's disk is full") {
		t.Fatalf("full disk: %v - want the plain 'disk is full' error", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("the earlier logo was damaged by the failed save")
	}
	if _, statErr := os.Stat(path + ".tmp"); statErr == nil {
		t.Fatal("a half-written temp file was left in the store")
	}
}

// 49.10.5: a bulk retention sweep writes WAL in bounded batches, so a sweep of
// many rows grows the log in proportion to the rows, a few MB per batch, never
// as one huge transaction.
func TestRetentionSweepWALGrowthIsBoundedPerBatch49105(t *testing.T) {
	db.InitDB(testConnStr())
	var probe string
	if err := db.DB.QueryRow(`SELECT pg_current_wal_lsn()::text`).Scan(&probe); err != nil {
		t.Skipf("WAL position not readable here: %v", err)
	}
	suffix := strings.ReplaceAll(filepath.Base(t.TempDir()), "-", "")
	if len(suffix) > 18 {
		suffix = suffix[len(suffix)-18:]
	}
	tenantID, schema := "wal"+suffix, "wal_schema"+suffix
	if _, err := db.DB.Exec(`INSERT INTO public.tenants (tenant_id, name, schema_name) VALUES ($1, $1, $2)`, tenantID, schema); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
	defer db.DB.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
	rows := 3 * retentionBatchRows
	for _, q := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.log (id SERIAL PRIMARY KEY, created_at TIMESTAMP NOT NULL)`,
		fmt.Sprintf(`INSERT INTO %s.log (created_at) SELECT NOW() - INTERVAL '400 days' FROM generate_series(1, %d)`, schema, rows),
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var before string
	db.DB.QueryRow(`SELECT pg_current_wal_lsn()::text`).Scan(&before)
	n, err := retentionDelete(tenantID, schema, "log", `created_at < CURRENT_TIMESTAMP - ($1 || ' days')::interval`, 90)
	if err != nil || n != int64(rows) {
		t.Fatalf("deleted %d of %d (err %v)", n, rows, err)
	}
	var grown float64
	if err := db.DB.QueryRow(`SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)`, before).Scan(&grown); err != nil {
		t.Fatal(err)
	}
	const perBatchCap = 8 << 20 // 8 MB per 5,000 narrow rows is far above what a batch writes
	if grown > float64(3*perBatchCap) {
		t.Fatalf("a %d-row sweep wrote %.1f MB of WAL; want under %d MB per batch", rows, grown/(1<<20), perBatchCap>>20)
	}
}
