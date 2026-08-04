package engines

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"custom_erp/db"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// QZ Tray silent printing (Stage 31.1).
//
// QZ Tray is a small Java service the operator installs on each packing PC.
// It listens on a local WebSocket and prints to a named OS printer, which is
// what lets a browser page print silently instead of going through the
// window.print() dialog every existing print path in this app uses.
//
// Every privileged call the browser makes to QZ must be signed by a
// certificate QZ trusts, or QZ shows an allow/deny prompt per call. The
// signing key therefore lives here, server-side, and never reaches the
// browser - the browser only ever gets the public certificate and a
// signature for one specific request.
//
// THE SIGNING CONTRACT (verified against QZ's own source, not guessed - this
// is the part every QZ integration gets wrong):
//
//	qz/auth/Certificate.java:
//	   verifier.update(StringUtils.getBytesUtf8(DigestUtils.sha256Hex(data)));
//	   ... verifier.verify(Base64.decodeBase64(signature))   // "SHA512withRSA"
//
//	qz/ws/PrintSocketClient.java:
//	   JSONObject copy = new JSONObject(message, new String[]{"call","params","timestamp"});
//	   certificate.isSignatureValid(algorithm, signature, copy.toString()...)
//
// So the tray takes the JSON {"call":..,"params":..,"timestamp":..}, hashes
// it to a LOWERCASE HEX SHA-256 STRING, and verifies the signature over the
// UTF-8 bytes of that hex string using RSA/SHA-512.
//
// The browser hashes; this file signs the hex string it is handed. We
// deliberately do NOT re-derive the hash here from a reconstructed JSON
// document: JSON serialisation differences (key order, escaping) between Go
// and the browser would silently produce a different hash and every print
// would fail signature validation. public/qz-print.js reproduces the
// official qz-tray.js byte-for-byte, and this signs whatever hash it sends.

const (
	// QZSignAlgorithm is sent to the tray as `signAlgorithm`. The tray
	// defaults to SHA1 when the field is absent (Certificate.Algorithm);
	// we always send SHA512 and sign to match.
	QZSignAlgorithm = "SHA512"

	qzKeyFileName  = "qz_private_key.pem"
	qzCertFileName = "qz_certificate.pem"
)

// qzHashPattern constrains what this server is willing to sign.
//
// The sign endpoint is, by construction, a signing oracle: an authenticated
// caller hands over a string and gets it signed by our private key. QZ's own
// reference backends sign whatever arrives. Restricting the input to exactly
// one SHA-256 hex digest means the oracle can only ever emit signatures over
// 32-byte hash-shaped values - it cannot be talked into signing an
// attacker-chosen document, which is the whole risk of a blind signer.
var qzHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type qzKeyMaterial struct {
	key     *rsa.PrivateKey
	certPEM string
	err     error
}

var (
	qzOnce     sync.Once
	qzMaterial qzKeyMaterial
)

// loadQZKeyMaterial resolves the signing key exactly once.
//
// Mirrors loadOrGenerateChannelCredentialKey (engines/channel_credentials.go):
// explicit env wins for production, otherwise a keypair is generated once and
// persisted outside the repo under the OS per-user config dir so it is stable
// across restarts. Unlike that function this never log.Fatalf's - printing is
// an optional convenience, and a server that cannot sign print jobs must
// still serve every other module.
func loadQZKeyMaterial() qzKeyMaterial {
	qzOnce.Do(func() {
		keyPath := os.Getenv("QZ_PRIVATE_KEY_PATH")
		certPath := os.Getenv("QZ_CERTIFICATE_PATH")

		if keyPath != "" && certPath != "" {
			qzMaterial = readQZKeyPair(keyPath, certPath)
			return
		}

		configDir, err := os.UserConfigDir()
		if err != nil {
			qzMaterial = qzKeyMaterial{err: fmt.Errorf("cannot determine user config dir for QZ key persistence: %w", err)}
			return
		}
		dir := filepath.Join(configDir, "custom_erp")
		keyPath = filepath.Join(dir, qzKeyFileName)
		certPath = filepath.Join(dir, qzCertFileName)

		if m := readQZKeyPair(keyPath, certPath); m.err == nil {
			qzMaterial = m
			return
		}

		qzMaterial = generateQZKeyPair(dir, keyPath, certPath)
	})
	return qzMaterial
}

func readQZKeyPair(keyPath, certPath string) qzKeyMaterial {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not read QZ private key %s: %w", keyPath, err)}
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not read QZ certificate %s: %w", certPath, err)}
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return qzKeyMaterial{err: fmt.Errorf("QZ private key %s is not valid PEM", keyPath)}
	}
	key, err := parseRSAPrivateKey(block.Bytes)
	if err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not parse QZ private key %s: %w", keyPath, err)}
	}
	return qzKeyMaterial{key: key, certPEM: string(certPEM)}
}

// parseRSAPrivateKey accepts both PKCS#1 ("RSA PRIVATE KEY") and PKCS#8
// ("PRIVATE KEY") encodings, since openssl emits either depending on the
// flags an operator happened to use when generating the pair by hand.
func parseRSAPrivateKey(der []byte) (*rsa.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("QZ signing key must be RSA, got %T", parsed)
	}
	return key, nil
}

// generateQZKeyPair mints a self-signed 2048-bit RSA certificate.
//
// QZ requires x509 / 2048-bit RSA for an override certificate. The generated
// certificate is what gets copied to each packing PC as override.crt - see
// docs/guides/QZ_PRINTING_SETUP.md. Regenerating it invalidates every PC's
// override.crt, so the file is only written when absent.
func generateQZKeyPair(dir, keyPath, certPath string) qzKeyMaterial {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not generate QZ signing key: %w", err)}
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not generate QZ certificate serial: %w", err)}
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Custom ERP Print Authority",
			Organization: []string{"Custom ERP"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		// QZ validates the certificate's date range on every signed call. Ten
		// years keeps this off the operations backlog; rotating early just
		// means redistributing override.crt.
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not create QZ certificate: %w", err)}
	}

	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := os.MkdirAll(dir, 0700); err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not create config dir for QZ key: %w", err)}
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not persist QZ private key: %w", err)}
	}
	if err := os.WriteFile(certPath, []byte(certPEM), 0644); err != nil {
		return qzKeyMaterial{err: fmt.Errorf("could not persist QZ certificate: %w", err)}
	}

	return qzKeyMaterial{key: key, certPEM: certPEM}
}

// QZCertificatePEM returns the public certificate the browser hands to QZ
// Tray during the WebSocket handshake. Public by nature - it is the
// counterpart of the key, and is also what ships to each PC as override.crt.
func QZCertificatePEM() (string, error) {
	m := loadQZKeyMaterial()
	if m.err != nil {
		return "", m.err
	}
	return m.certPEM, nil
}

// QZSignRequest signs one SHA-256 hex digest for the browser.
//
// hashHex is produced by public/qz-print.js as
// sha256Hex(JSON.stringify({call, params, timestamp})) - see the signing
// contract at the top of this file. The returned signature is base64, which
// is what the tray's Base64.decodeBase64 expects.
func QZSignRequest(hashHex string) (string, error) {
	hashHex = strings.TrimSpace(strings.ToLower(hashHex))
	if !qzHashPattern.MatchString(hashHex) {
		return "", &ValidationError{
			Code:    "DEVICE-0299",
			Message: "print signing request must be a single SHA-256 hex digest",
		}
	}

	m := loadQZKeyMaterial()
	if m.err != nil {
		return "", m.err
	}

	// Sign the UTF-8 bytes of the hex string itself, NOT the 32 raw digest
	// bytes it represents - Certificate.java hashes to hex and calls
	// getBytesUtf8 on the result before verifying.
	digest := sha512.Sum512([]byte(hashHex))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA512, digest[:])
	if err != nil {
		return "", fmt.Errorf("could not sign QZ print request: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// QZPrinter is one row of the Printer Master, narrowed to what the print
// pipeline needs. Reuses the existing doctype (extended in
// db/migrations_stage31_1_qz_print.sql) rather than a parallel registry, so
// printers stay manageable from the normal Master Definition screens.
type QZPrinter struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	QZPrinterName string `json:"qz_printer_name"`
	PrintRole     string `json:"print_role"`
	Language      string `json:"printer_language"`
	Location      string `json:"location"`
	WidthMM       string `json:"label_width_mm"`
	HeightMM      string `json:"label_height_mm"`
	DPI           string `json:"dpi"`
}

// ListQZPrinters returns every Active Printer for the tenant, so the browser
// can resolve a print role (Shipping Label / Invoice / ...) to an OS printer
// name without asking the operator to pick one per job.
func ListQZPrinters(tenantID string) ([]QZPrinter, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id,
		       COALESCE(data->>'code', ''),
		       COALESCE(data->>'name', ''),
		       COALESCE(data->>'qz_printer_name', ''),
		       COALESCE(data->>'print_role', ''),
		       COALESCE(data->>'printer_language', ''),
		       COALESCE(data->>'location', ''),
		       COALESCE(data->>'label_width_mm', ''),
		       COALESCE(data->>'label_height_mm', ''),
		       COALESCE(data->>'dpi', '')
		FROM %s.documents
		WHERE doctype = 'Printer' AND deleted_at IS NULL
		  AND COALESCE(data->>'status', 'Active') = 'Active'
		ORDER BY COALESCE(data->>'print_role', ''), COALESCE(data->>'code', '')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	printers := []QZPrinter{}
	for rows.Next() {
		var p QZPrinter
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.QZPrinterName, &p.PrintRole,
			&p.Language, &p.Location, &p.WidthMM, &p.HeightMM, &p.DPI); err != nil {
			return nil, err
		}
		printers = append(printers, p)
	}
	return printers, rows.Err()
}

// LogPrintJob records one submitted (or failed) QZ job.
//
// Best-effort by design: a logging failure must never lose the operator the
// label they just printed, so the error is returned for the caller to note
// but the print itself is already away.
func LogPrintJob(tenantID, jobType, documentRef, printerCode, qzPrinterName, format string,
	copies int, printedBy, status, errorDetail string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if copies < 1 {
		copies = 1
	}
	if status == "" {
		status = "Submitted"
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.print_job_log
		  (job_type, document_ref, printer_code, qz_printer_name, print_format, copies, printed_by, status, error_detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''))`, schema),
		jobType, documentRef, printerCode, qzPrinterName, format, copies, printedBy, status, errorDetail)
	return err
}

// PrintJobLogEntry is one row of the print audit trail.
type PrintJobLogEntry struct {
	JobType       string    `json:"job_type"`
	DocumentRef   string    `json:"document_ref"`
	PrinterCode   string    `json:"printer_code"`
	QZPrinterName string    `json:"qz_printer_name"`
	Format        string    `json:"print_format"`
	Copies        int       `json:"copies"`
	PrintedBy     string    `json:"printed_by"`
	Status        string    `json:"status"`
	ErrorDetail   string    `json:"error_detail"`
	PrintedAt     time.Time `json:"printed_at"`
}

// GetPrintJobLog returns the most recent print jobs, newest first.
func GetPrintJobLog(tenantID string, limit int) ([]PrintJobLogEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT job_type, COALESCE(document_ref, ''), COALESCE(printer_code, ''),
		       COALESCE(qz_printer_name, ''), COALESCE(print_format, ''), copies,
		       COALESCE(printed_by, ''), status, COALESCE(error_detail, ''), printed_at
		FROM %s.print_job_log
		ORDER BY printed_at DESC
		LIMIT $1`, schema), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []PrintJobLogEntry{}
	for rows.Next() {
		var e PrintJobLogEntry
		if err := rows.Scan(&e.JobType, &e.DocumentRef, &e.PrinterCode, &e.QZPrinterName,
			&e.Format, &e.Copies, &e.PrintedBy, &e.Status, &e.ErrorDetail, &e.PrintedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
