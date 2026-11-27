package engines

import (
	"bytes"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// 297.2 - the resumable upload. A piece sent twice changes nothing, a gap is
// refused with where to carry on from, an overlap keeps only the new bytes,
// and the whole file's SHA-256 is known the moment its last byte lands.

func uploadTestDir(t *testing.T) {
	t.Helper()
	prev, prevProbe := mediaStoreDir, uploadDiskProbe
	mediaStoreDir = t.TempDir()
	uploadDiskProbe = func() (uint64, bool) { return 1 << 40, true }
	t.Cleanup(func() { mediaStoreDir, uploadDiskProbe = prev, prevProbe })
}

// sendUpload starts an upload of body for owner and sends it in pieces of
// piece bytes, returning its id.
func sendUpload(t *testing.T, tenantID, owner, name string, body []byte, piece int) string {
	t.Helper()
	u, err := StartResumableUpload(tenantID, owner, "import", name, int64(len(body)))
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	for at := 0; at < len(body); at += piece {
		end := at + piece
		if end > len(body) {
			end = len(body)
		}
		if _, err := PutResumableUploadPiece(tenantID, owner, u.ID, int64(at), bytes.NewReader(body[at:end])); err != nil {
			t.Fatalf("piece at %d: %v", at, err)
		}
	}
	return u.ID
}

func TestImportUploadPiecesGapsReplaysAndHash(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	body := bytes.Repeat([]byte("0123456789abcdef"), 1000) // 16,000 bytes
	u, err := StartResumableUpload(tenantID, "stg_maker", "", `C:\Users\x\big file.csv`, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if u.Purpose != "import" || u.FileName != "big file.csv" || u.Status != uploadReceiving {
		t.Fatalf("started %+v; want purpose import, the bare file name, Receiving", u)
	}
	put := func(off int64, b []byte) (*ResumableUpload, error) {
		return PutResumableUploadPiece(tenantID, "stg_maker", u.ID, off, bytes.NewReader(b))
	}

	if got, err := put(0, body[:4000]); err != nil || got.Received != 4000 {
		t.Fatalf("first piece: %v / %+v", err, got)
	}
	// The same piece again (its answer was lost): nothing written twice.
	if got, err := put(0, body[:4000]); err != nil || got.Received != 4000 {
		t.Fatalf("replayed piece: %v / %+v", err, got)
	}
	// A piece past what the server has: refused, saying where to carry on.
	var gap ErrUploadOffset
	if _, err := put(6000, body[6000:7000]); !errors.As(err, &gap) || gap.Received != 4000 {
		t.Fatalf("gap: %v, want ErrUploadOffset{4000}", err)
	}
	// An overlap: only the bytes the server does not have are kept.
	if got, err := put(3000, body[3000:9000]); err != nil || got.Received != 9000 {
		t.Fatalf("overlap: %v / %+v", err, got)
	}
	// Another user cannot see or add to it.
	if _, err := GetResumableUpload(tenantID, "stg_checker", u.ID); stagingCode(err) != "DATAIM-0489" {
		t.Fatalf("another user's GET: %v, want DATAIM-0489", err)
	}
	if _, err := PutResumableUploadPiece(tenantID, "stg_checker", u.ID, 9000, bytes.NewReader(body[9000:])); stagingCode(err) != "DATAIM-0489" {
		t.Fatalf("another user's piece: %v, want DATAIM-0489", err)
	}
	// A piece running past the size it was started with.
	if _, err := put(9000, append(append([]byte{}, body[9000:]...), 'x')); stagingCode(err) != "DATAIM-0488" {
		t.Fatalf("piece past the end: %v, want DATAIM-0488", err)
	}
	// A piece over the limit, before anything is locked.
	if _, err := put(9000, make([]byte, ResumableUploadPieceMax+1)); stagingCode(err) != "DATAIM-0488" {
		t.Fatalf("oversize piece: %v, want DATAIM-0488", err)
	}
	if _, err := put(-1, body[:1]); stagingCode(err) != "DATAIM-0488" {
		t.Fatalf("negative offset: %v, want DATAIM-0488", err)
	}

	done, err := put(9000, body[9000:])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if done.Status != uploadComplete || done.Received != int64(len(body)) || done.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("complete: %+v; want Complete, %d bytes, sha %x", done, len(body), sum)
	}
	onDisk, err := os.ReadFile(resumableUploadPath(schema, u.ID))
	if err != nil || !bytes.Equal(onDisk, body) {
		t.Fatalf("the file on disk is not the file sent (%v, %d bytes)", err, len(onDisk))
	}
	// The last piece again after the answer was lost: done already.
	if again, err := put(9000, body[9000:]); err != nil || again.Status != uploadComplete {
		t.Fatalf("last piece again: %v / %+v", err, again)
	}
}

func TestImportUploadLimits(t *testing.T) {
	tenantID, schema := stagingTenant(t)
	uploadTestDir(t)
	if _, err := StartResumableUpload(tenantID, "stg_maker", "import", "a.csv", 0); stagingCode(err) != "DATAIM-0163" {
		t.Fatalf("empty: %v, want DATAIM-0163", err)
	}
	if _, err := StartResumableUpload(tenantID, "stg_maker", "import", "a.csv", resumableUploadMaxBytes+1); stagingCode(err) != "DATAIM-0490" {
		t.Fatalf("too large: %v, want DATAIM-0490", err)
	}
	if _, err := StartResumableUpload(tenantID, "", "import", "a.csv", 10); stagingCode(err) != "DATAIM-0489" {
		t.Fatalf("no owner: %v, want DATAIM-0489", err)
	}
	// No room on the disk for the file and the import it feeds.
	uploadDiskProbe = func() (uint64, bool) { return 3 << 30, true }
	if _, err := StartResumableUpload(tenantID, "stg_maker", "import", "a.csv", 200<<20); stagingCode(err) != "DATAIM-0490" {
		t.Fatalf("no disk room: %v, want DATAIM-0490", err)
	}
	uploadDiskProbe = func() (uint64, bool) { return 1 << 40, true }

	// Uploads still going, per user.
	var ids []string
	for i := 0; i < resumableUploadOpenMax; i++ {
		u, err := StartResumableUpload(tenantID, "stg_maker", "import", fmt.Sprintf("f%d.csv", i), 10)
		if err != nil {
			t.Fatalf("upload %d: %v", i, err)
		}
		ids = append(ids, u.ID)
	}
	if _, err := StartResumableUpload(tenantID, "stg_maker", "import", "one-more.csv", 10); stagingCode(err) != "DATAIM-0490" {
		t.Fatalf("one upload too many: %v, want DATAIM-0490", err)
	}
	if _, err := StartResumableUpload(tenantID, "stg_checker", "import", "theirs.csv", 10); err != nil {
		t.Fatalf("another user's upload was counted against this one's: %v", err)
	}

	// Left over a day: the sweep removes the bytes, and the upload is gone.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.resumable_uploads SET updated_at = $2 WHERE id = $1`, schema), ids[0], time.Now().Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, err := sweepResumableUploads(schema, time.Now()); err != nil || n != 1 {
		t.Fatalf("sweep: %d, %v; want 1", n, err)
	}
	if _, err := os.Stat(resumableUploadPath(schema, ids[0])); !os.IsNotExist(err) {
		t.Fatalf("the swept upload's bytes are still on disk: %v", err)
	}
	if _, err := PutResumableUploadPiece(tenantID, "stg_maker", ids[0], 0, bytes.NewReader([]byte("x"))); stagingCode(err) != "DATAIM-0489" {
		t.Fatalf("piece to a swept upload: %v, want DATAIM-0489", err)
	}
	if _, err := StartResumableUpload(tenantID, "stg_maker", "import", "now-there-is-room.csv", 10); err != nil {
		t.Fatalf("after the sweep freed a place: %v", err)
	}
}
