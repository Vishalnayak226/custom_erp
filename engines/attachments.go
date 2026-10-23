package engines

import (
	"bytes"
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Stage 55.15b: document attachments - a small store for files that belong to
// a document, first used for expense receipts.
//
// It reuses the media store rather than adding a second storage layer: the
// bytes live under mediaStoreDir (engines/pim_media.go - private, never under
// public/, served only through an authenticated handler) in an
// attachments/<tenant schema>/ folder, under a random 128-bit name, so a file
// name on disk says nothing about the document it belongs to and cannot be
// guessed. The document_attachments table records what each file is.
//
// What a file is decides whether it is accepted - its bytes are sniffed, never
// its name or the type the browser declared - and only JPEG, PNG, WebP and PDF
// get in. Who may read a file is not decided here at all: it is whoever may
// read the document it is attached to, which the HTTP layer checks with the
// same rule as GET /api/v1/doc (internal/server/handlers_attachments.go).
// This engine owns what the file and its document must be: the per-file and
// per-document limits, duplicates, and which document statuses still allow a
// file to be added or removed.

const (
	// MaxAttachmentBytes is the largest single file accepted (5 MB).
	MaxAttachmentBytes = 5 << 20
	// MaxAttachmentsPerDocument is how many live files one document may hold.
	MaxAttachmentsPerDocument = 5
)

// attachmentDoctypes is the allow-list of doctypes that take attachments,
// each with the document statuses in which a file may still be added or
// removed. Enabling another doctype is one line here (plus its UI).
var attachmentDoctypes = map[string]map[string]bool{
	"ExpenseClaim": {"Draft": true, "Rejected": true},
	// Stage 74.1.6: evidence on stock and purchase documents - the vendor's
	// invoice copy on a goods receipt while it is worked in steps (94.2), the
	// proof on a purchase return before it posts.
	"GRN":            {"Pending": true},
	"PurchaseReturn": {"Draft": true},
}

// attachmentTypes are the only contents accepted, keyed by the sniffed type,
// with the extension the stored copy gets.
var attachmentTypes = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

var (
	attachmentIDPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	attachmentKeyPattern = regexp.MustCompile(`^attachments/[a-z0-9_]+/[0-9a-f]{32}\.(jpg|png|webp|pdf)$`)
)

// DocumentAttachment is one file on one document. The storage key is never
// sent to a client.
type DocumentAttachment struct {
	ID          string    `json:"id"`
	Doctype     string    `json:"doctype"`
	DocumentID  string    `json:"document_id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	UploadedBy  string    `json:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at"`
	storageKey  string
}

// AttachmentParent is the document an attachment belongs to, as stored.
type AttachmentParent struct {
	ID     string
	Status string
	Data   map[string]interface{}
}

// AttachmentsEnabled reports whether a doctype takes attachments.
func AttachmentsEnabled(doctype string) bool {
	_, ok := attachmentDoctypes[doctype]
	return ok
}

// AttachmentStatusAllowsChange reports whether a document in this status may
// still have files added or removed.
func AttachmentStatusAllowsChange(doctype, status string) bool {
	return attachmentDoctypes[doctype][status]
}

// SetMediaStoreDirForTest points the media store (and with it every
// attachment) at dir. Tests outside this package call it for the reason
// mediaStoreDir's own comment gives; the server never does.
func SetMediaStoreDirForTest(dir string) { mediaStoreDir = dir }

func attachmentError(code, message string) error {
	return &ValidationError{Code: code, Message: message}
}

var errAttachmentNotFound = &ValidationError{Code: "GLOBAL-0004", Message: "That file was not found - it may have been removed."}

// sniffAttachmentType decides what a file is from its bytes alone. JPEG and
// PNG must also carry a readable image header, so a few magic bytes in front
// of anything else do not pass as a picture.
func sniffAttachmentType(content []byte) (contentType, ext string, err error) {
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	detected := http.DetectContentType(head)
	if i := strings.IndexByte(detected, ';'); i >= 0 {
		detected = strings.TrimSpace(detected[:i])
	}
	ext, ok := attachmentTypes[detected]
	if !ok {
		return "", "", attachmentError("EXP-0541", "Its content is not a JPEG, PNG or WebP picture or a PDF.")
	}
	if detected == "image/jpeg" || detected == "image/png" {
		if _, _, decErr := image.DecodeConfig(bytes.NewReader(content)); decErr != nil {
			return "", "", attachmentError("EXP-0541", "It looks like a picture but cannot be read as one.")
		}
	}
	if strings.HasPrefix(detected, "image/") {
		if err := CheckUpload(UploadImage, content); err != nil { // 134.4: size in pixels, polyglots
			return "", "", attachmentError("EXP-0541", err.(*ValidationError).Message)
		}
	}
	return detected, ext, nil
}

// cleanAttachmentName keeps the name the user gave the file, minus any folder
// part and control or quote characters, and makes sure it ends in the
// extension of what the file really is.
func cleanAttachmentName(name, ext string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = "receipt"
	}
	current := strings.ToLower(path.Ext(name))
	if current != ext && !(ext == ".jpg" && current == ".jpeg") {
		name += ext
	}
	if len(name) > 200 {
		e := path.Ext(name)
		stem := strings.TrimSuffix(name, e)
		for len(stem)+len(e) > 200 && stem != "" {
			_, size := utf8.DecodeLastRuneInString(stem)
			stem = stem[:len(stem)-size]
		}
		name = stem + e
		if len(name) > 200 || stem == "" {
			name = "receipt" + ext
		}
	}
	return name
}

func newAttachmentStorageKey(schema, ext string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "attachments/" + schema + "/" + hex.EncodeToString(raw) + ext, nil
}

// checkAttachmentKey refuses any storage key this engine could not have
// written, before the media store (media_store.go) is asked for it.
func checkAttachmentKey(key string) error {
	if !attachmentKeyPattern.MatchString(key) {
		return fmt.Errorf("attachment storage key is not valid")
	}
	return nil
}

// AddAttachment stores a file on a document. The document is locked while
// its status, file count and duplicates are checked, so two uploads at once
// cannot both slip under the limit.
func AddAttachment(tenantID, doctype, documentID, fileName string, content []byte, uploadedBy string) (*DocumentAttachment, error) {
	if !AttachmentsEnabled(doctype) {
		return nil, attachmentError("GLOBAL-0011", "Files cannot be attached to this kind of record.")
	}
	if len(content) == 0 {
		return nil, attachmentError("EXP-0541", "The file is empty.")
	}
	if len(content) > MaxAttachmentBytes {
		return nil, attachmentError("EXP-0542", fmt.Sprintf("It is %.1f MB; one file can be at most 5 MB.", float64(len(content))/(1<<20)))
	}
	contentType, ext, err := sniffAttachmentType(content)
	if err != nil {
		return nil, err
	}
	sum := checksumOf(content)
	name := cleanAttachmentName(fileName, ext)

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema), doctype, documentID).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, attachmentError("GLOBAL-0004", "That record was not found - it may have been deleted.")
	}
	if err != nil {
		return nil, err
	}
	if !AttachmentStatusAllowsChange(doctype, status) {
		return nil, attachmentError("EXP-0545", fmt.Sprintf("Its status is %s.", status))
	}
	var live, same int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE sha256 = $3)
		FROM %s.document_attachments WHERE doctype = $1 AND document_id = $2 AND deleted_at IS NULL`, schema),
		doctype, documentID, sum).Scan(&live, &same); err != nil {
		return nil, err
	}
	if same > 0 {
		return nil, attachmentError("EXP-0544", "The same file is already on this record.")
	}
	if live >= MaxAttachmentsPerDocument {
		return nil, attachmentError("EXP-0543", fmt.Sprintf("It has %d files; the most is %d.", live, MaxAttachmentsPerDocument))
	}

	key, err := newAttachmentStorageKey(schema, ext)
	if err != nil {
		return nil, err
	}
	if err := checkAttachmentKey(key); err != nil {
		return nil, err
	}
	// Stage 75.4.1: written once, private, through the server's media store.
	if err := serverMedia.Write(key, content, MediaWrite{Exclusive: true, Private: true}); err != nil {
		return nil, fmt.Errorf("failed to store the file: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = serverMedia.Remove(key)
		}
	}()

	att := &DocumentAttachment{
		Doctype: doctype, DocumentID: documentID, FileName: name, ContentType: contentType,
		SizeBytes: int64(len(content)), SHA256: sum, UploadedBy: uploadedBy,
	}
	if err := tx.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.document_attachments (doctype, document_id, file_name, content_type, size_bytes, sha256, storage_key, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`, schema),
		doctype, documentID, name, contentType, att.SizeBytes, sum, key, uploadedBy).Scan(&att.ID, &att.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return att, nil
}

const attachmentColumns = `id, doctype, document_id, file_name, content_type, size_bytes, sha256, uploaded_by, created_at, storage_key`

func scanAttachment(row interface{ Scan(...interface{}) error }) (*DocumentAttachment, error) {
	var a DocumentAttachment
	if err := row.Scan(&a.ID, &a.Doctype, &a.DocumentID, &a.FileName, &a.ContentType, &a.SizeBytes, &a.SHA256, &a.UploadedBy, &a.CreatedAt, &a.storageKey); err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAttachments returns a document's live files, oldest first.
func ListAttachments(tenantID, doctype, documentID string) ([]DocumentAttachment, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.document_attachments
		WHERE doctype = $1 AND document_id = $2 AND deleted_at IS NULL ORDER BY created_at, id`, attachmentColumns, schema), doctype, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentAttachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// CountAttachments returns how many live files each listed document holds;
// a document with none is absent from the map.
func CountAttachments(tenantID, doctype string, documentIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(documentIDs) == 0 {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT document_id, COUNT(*) FROM %s.document_attachments
		WHERE doctype = $1 AND document_id = ANY($2) AND deleted_at IS NULL GROUP BY document_id`, schema), doctype, pq.Array(documentIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// GetAttachment returns one live file's record (not its bytes), so the caller
// can authorize against its document before anything else is revealed.
func GetAttachment(tenantID, attachmentID string) (*DocumentAttachment, error) {
	if !attachmentIDPattern.MatchString(attachmentID) {
		return nil, errAttachmentNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	a, err := scanAttachment(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.document_attachments WHERE id = $1 AND deleted_at IS NULL`, attachmentColumns, schema), attachmentID))
	if err == sql.ErrNoRows {
		return nil, errAttachmentNotFound
	}
	return a, err
}

// OpenAttachment returns a live file's record and an open handle on its
// bytes. The caller closes the file.
func OpenAttachment(tenantID, attachmentID string) (*DocumentAttachment, io.ReadSeekCloser, error) {
	a, err := GetAttachment(tenantID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	if err := checkAttachmentKey(a.storageKey); err != nil {
		return nil, nil, err
	}
	f, err := serverMedia.Open(a.storageKey)
	if err != nil {
		return nil, nil, fmt.Errorf("the stored file is missing: %v", err)
	}
	return a, f, nil
}

// RemoveAttachment soft-deletes a file: the row and the stored bytes stay for
// audit, and the file stops being listed or served. Only the person who added
// it may remove it, and only while the document still allows changes; a
// Super Admin may remove any file at any status (a wrong or sensitive upload
// has to be removable after submission too).
func RemoveAttachment(tenantID, attachmentID, actorUserID string, superAdmin bool) (*DocumentAttachment, error) {
	if !attachmentIDPattern.MatchString(attachmentID) {
		return nil, errAttachmentNotFound
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

	a, err := scanAttachment(tx.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.document_attachments WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, attachmentColumns, schema), attachmentID))
	if err == sql.ErrNoRows {
		return nil, errAttachmentNotFound
	}
	if err != nil {
		return nil, err
	}
	var status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), a.Doctype, a.DocumentID).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if !superAdmin {
		if a.UploadedBy != actorUserID {
			return nil, attachmentError("GLOBAL-0011", "Only the person who added this file can remove it.")
		}
		if !AttachmentStatusAllowsChange(a.Doctype, status) {
			return nil, attachmentError("EXP-0545", fmt.Sprintf("Its status is %s.", status))
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.document_attachments SET deleted_at = CURRENT_TIMESTAMP, deleted_by = $2 WHERE id = $1`, schema), attachmentID, actorUserID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

// LoadAttachmentParents loads the listed documents of one doctype as stored
// (data, status), skipping deleted ones - what the HTTP layer authorizes
// against before it touches any attachment.
func LoadAttachmentParents(tenantID, doctype string, ids []string) (map[string]AttachmentParent, error) {
	out := map[string]AttachmentParent{}
	if len(ids) == 0 {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = $1 AND id = ANY($2) AND deleted_at IS NULL`, schema), doctype, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, dataStr, status string
		if err := rows.Scan(&id, &dataStr, &status); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil || data == nil {
			continue // corrupt row: never authorizable, so never visible
		}
		data["id"] = id
		data["status"] = status
		out[id] = AttachmentParent{ID: id, Status: status, Data: data}
	}
	return out, rows.Err()
}
