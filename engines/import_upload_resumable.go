package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 297.2: a bulk upload that never fails. A file of any size goes up in
// pieces of at most 1 MiB. Each piece names the byte it starts at; the server
// writes only bytes it does not have yet, so a piece sent twice (a retry
// after a dropped line) changes nothing, and a gap is refused with how far
// the server has got - the browser carries on from there. The file sits on
// the server's disk under media_store/uploads/<client>/, and the row in
// resumable_uploads keeps how far it has got and the running SHA-256, so the
// whole file's hash is known the moment its last byte lands and nothing is
// read twice. A tab closed, a laptop asleep or a server restarted: the
// upload resumes where it stopped.
//
// An upload is the user's own: another user's upload id is not found.

const (
	ResumableUploadPieceMax  = 1 << 20 // bytes in one PUT
	resumableUploadMaxBytes  = 4 << 30 // one file
	resumableUploadOpenMax   = 5       // uploads still receiving, per user
	resumableUploadIdle      = 24 * time.Hour
	resumableUploadDiskRatio = 8 // disk an import needs per byte of file: rows, two JSON copies, indexes, WAL
)

// Upload statuses.
const (
	uploadReceiving = "Receiving"
	uploadComplete  = "Complete"
	uploadUsed      = "Used"
	uploadExpired   = "Expired"
)

// ResumableUpload is an upload as the API returns it.
type ResumableUpload struct {
	ID       string `json:"id"`
	FileName string `json:"file_name"`
	Purpose  string `json:"purpose"`
	Size     int64  `json:"size"`
	Received int64  `json:"received"`
	Status   string `json:"status"`
	SHA256   string `json:"sha256,omitempty"`
}

// ErrUploadOffset is a piece that does not start where the server got to:
// Received says where to carry on from.
type ErrUploadOffset struct{ Received int64 }

func (e ErrUploadOffset) Error() string {
	return fmt.Sprintf("the server has the first %d bytes of this file - send from there", e.Received)
}

func resumableUploadDir(schema string) string {
	return filepath.Join(mediaStoreDir, "uploads", schema)
}

func resumableUploadPath(schema, id string) string {
	return filepath.Join(resumableUploadDir(schema), id+".part")
}

// resumableUploadText is the file converted to UTF-8, when it came in another
// encoding (UTF-16 or Windows-1252): converted once, then read like any other.
func resumableUploadText(schema, id string) string {
	return filepath.Join(resumableUploadDir(schema), id+".utf8")
}

// removeUploadFiles removes an upload's bytes and any converted copy.
func removeUploadFiles(schema, id string) {
	_ = os.Remove(resumableUploadPath(schema, id))
	_ = os.Remove(resumableUploadText(schema, id))
}

// uploadDiskProbe is the free bytes of the disk uploads land on; a test
// replaces it.
var uploadDiskProbe = func() (uint64, bool) {
	free, _, err := storageDiskProbe()
	return free, err == nil
}

// uploadDiskRoom is whether an upload of size bytes, and the import it feeds,
// fit on the server's disk with a reserve left for everything else.
func uploadDiskRoom(size int64) error {
	free, ok := uploadDiskProbe()
	if !ok {
		return nil // a disk that cannot be read is not a reason to refuse
	}
	const reserve = 2 << 30
	need := uint64(size)*resumableUploadDiskRatio + reserve
	if free < need {
		room := int64(0)
		if free > reserve {
			room = int64((free - reserve) / resumableUploadDiskRatio)
		}
		return importBatchErr("DATAIM-0490", "the server has room to import about %d MB right now; this file is %d MB - split it or ask your administrator to free disk space",
			room>>20, size>>20)
	}
	return nil
}

func newUploadID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "UPL" + hex.EncodeToString(b[:])
}

func sha256State(h interface{}) []byte {
	state, _ := h.(encoding.BinaryMarshaler).MarshalBinary()
	return state
}

// StartResumableUpload opens an upload of size bytes for owner.
func StartResumableUpload(tenantID, owner, purpose, fileName string, size int64) (*ResumableUpload, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, importBatchErr("DATAIM-0489", "sign in to upload")
	}
	if size <= 0 {
		return nil, importBatchErr("DATAIM-0163", "the file is empty")
	}
	if size > resumableUploadMaxBytes {
		return nil, importBatchErr("DATAIM-0490", "the file is larger than %d GB - split it into smaller files", resumableUploadMaxBytes>>30)
	}
	if purpose == "" {
		purpose = "import"
	}
	fileName = truncateRunes(strings.TrimSpace(filepath.Base(strings.ReplaceAll(fileName, `\`, "/"))), 255)
	if err := uploadDiskRoom(size); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var open int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.resumable_uploads WHERE owner = $1 AND status = $2 AND updated_at > $3`, schema),
		owner, uploadReceiving, time.Now().Add(-resumableUploadIdle)).Scan(&open); err != nil {
		return nil, err
	}
	if open >= resumableUploadOpenMax {
		return nil, importBatchErr("DATAIM-0490", "you have %d uploads still going - finish or leave one for a day before starting another", open)
	}
	id := newUploadID()
	if err := os.MkdirAll(resumableUploadDir(schema), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(resumableUploadPath(schema, id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.resumable_uploads (id, owner, purpose, file_name, size_bytes, hash_state) VALUES ($1, $2, $3, $4, $5, $6)`, schema),
		id, owner, purpose, fileName, size, sha256State(sha256.New())); err != nil {
		_ = os.Remove(resumableUploadPath(schema, id))
		return nil, err
	}
	return &ResumableUpload{ID: id, FileName: fileName, Purpose: purpose, Size: size, Status: uploadReceiving}, nil
}

type uploadRow struct {
	ResumableUpload
	owner     string
	hashState []byte
}

func scanUploadRow(row *sql.Row) (*uploadRow, error) {
	u := &uploadRow{}
	err := row.Scan(&u.ID, &u.owner, &u.Purpose, &u.FileName, &u.Size, &u.Received, &u.Status, &u.SHA256, &u.hashState)
	if err == sql.ErrNoRows {
		return nil, importBatchErr("DATAIM-0489", "that upload was not found - it may have been left for over a day; start it again")
	}
	return u, err
}

const uploadCols = `id, owner, purpose, file_name, size_bytes, received_bytes, status, sha256, hash_state`

// GetResumableUpload is how far an upload has got.
func GetResumableUpload(tenantID, owner, id string) (*ResumableUpload, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	u, err := scanUploadRow(db.DB.QueryRow(fmt.Sprintf(`SELECT `+uploadCols+` FROM %s.resumable_uploads WHERE id = $1 AND owner = $2`, schema), id, owner))
	if err != nil {
		return nil, err
	}
	return &u.ResumableUpload, nil
}

// PutResumableUploadPiece writes one piece that starts at offset. Bytes the
// server already has are skipped; a piece that starts past them is refused
// with ErrUploadOffset.
func PutResumableUploadPiece(tenantID, owner, id string, offset int64, body io.Reader) (*ResumableUpload, error) {
	// The piece is read before the row is locked, so a slow line never holds it.
	piece, err := io.ReadAll(io.LimitReader(body, ResumableUploadPieceMax+1))
	if err != nil {
		return nil, err
	}
	if len(piece) > ResumableUploadPieceMax {
		return nil, importBatchErr("DATAIM-0488", "send at most %d KB at a time", ResumableUploadPieceMax>>10)
	}
	if offset < 0 {
		return nil, importBatchErr("DATAIM-0488", "the piece's start is not a byte of the file")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u, err := scanUploadRow(tx.QueryRow(fmt.Sprintf(`SELECT `+uploadCols+` FROM %s.resumable_uploads WHERE id = $1 AND owner = $2 FOR UPDATE`, schema), id, owner))
	if err != nil {
		return nil, err
	}
	if u.Status != uploadReceiving {
		if u.Status == uploadComplete || u.Status == uploadUsed {
			// The last piece again, after the answer to it was lost: done already.
			return &u.ResumableUpload, nil
		}
		return nil, importBatchErr("DATAIM-0489", "that upload was left for over a day and has been removed; start it again")
	}
	if offset > u.Received {
		return nil, ErrUploadOffset{Received: u.Received}
	}
	if skip := u.Received - offset; skip > 0 {
		if skip >= int64(len(piece)) {
			return &u.ResumableUpload, nil // all of it is here already
		}
		piece = piece[skip:]
	}
	if u.Received+int64(len(piece)) > u.Size {
		return nil, importBatchErr("DATAIM-0488", "this piece goes past the %d bytes the upload was started with", u.Size)
	}
	if len(piece) == 0 {
		return &u.ResumableUpload, nil
	}
	h := sha256.New()
	if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(u.hashState); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(resumableUploadPath(schema, id), os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	// WriteAt at the received mark: bytes a crash left past it are overwritten.
	if _, err := f.WriteAt(piece, u.Received); err != nil {
		_ = f.Close()
		return nil, err
	}
	u.Received += int64(len(piece))
	if u.Received == u.Size {
		_ = f.Truncate(u.Size)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	_, _ = h.Write(piece)
	if u.Received == u.Size {
		u.Status = uploadComplete
		u.SHA256 = hex.EncodeToString(h.Sum(nil))
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.resumable_uploads SET received_bytes = $2, status = $3, sha256 = $4, hash_state = $5, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema),
		id, u.Received, u.Status, u.SHA256, sha256State(h)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &u.ResumableUpload, nil
}

// completeUpload is a whole upload owned by owner, ready to read.
func completeUpload(schema, owner, id string) (*uploadRow, error) {
	u, err := scanUploadRow(db.DB.QueryRow(fmt.Sprintf(`SELECT `+uploadCols+` FROM %s.resumable_uploads WHERE id = $1 AND owner = $2`, schema), id, owner))
	if err != nil {
		return nil, err
	}
	if u.Status != uploadComplete {
		return nil, importBatchErr("DATAIM-0489", "that upload is %s, not complete - %d of %d bytes are here", strings.ToLower(u.Status), u.Received, u.Size)
	}
	return u, nil
}

// markUploadUsed ends an upload whose file has been read in full: the row
// stays (what was uploaded, by whom), the bytes go.
func markUploadUsed(schema, id string) {
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.resumable_uploads SET status = $2, hash_state = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), id, uploadUsed); err == nil {
		removeUploadFiles(schema, id)
	}
}

// sweepResumableUploads removes the bytes of uploads left for over a day,
// finished or not, unless an import is still reading them.
func sweepResumableUploads(schema string, now time.Time) (int64, error) {
	var tbl sql.NullString
	if err := db.DB.QueryRow(`SELECT to_regclass($1)::text`, schema+".resumable_uploads").Scan(&tbl); err != nil || !tbl.Valid {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT u.id FROM %[1]s.resumable_uploads u
		WHERE u.status IN ('Receiving', 'Complete') AND u.updated_at < $1
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents d WHERE d.doctype = 'ImportBatch' AND d.deleted_at IS NULL
		                  AND d.data->'work'->>'upload_id' = u.id AND d.data->>'stage' = 'staging')`, schema), now.Add(-resumableUploadIdle))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var n int64
	for _, id := range ids {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.resumable_uploads SET status = $2, hash_state = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), id, uploadExpired); err != nil {
			return n, err
		}
		removeUploadFiles(schema, id)
		n++
	}
	return n, nil
}
