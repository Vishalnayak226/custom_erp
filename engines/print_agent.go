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
	"encoding/json"
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

// Silent printing through the print agent (Stage 31.1; PrintBridge since
// 31.1.9, names from ask 263).
//
// PrintBridge is the small service the operator installs on each PC. It
// listens on a local WebSocket and prints to a named OS printer, which is
// what lets a browser page print silently instead of going through the
// window.print() dialog every other print path in this app uses. It speaks
// the QZ Tray protocol, so a marketplace panel that needs QZ Tray works with
// the same signing below.
//
// Every privileged call the browser makes to the agent must be signed by a
// certificate the agent trusts, or it asks allow/deny per call. The
// signing key therefore lives here, server-side, and never reaches the
// browser - the browser only ever gets the public certificate and a
// signature for one specific request.
//
// THE SIGNING CONTRACT (the QZ Tray protocol, verified against QZ's own
// source, not guessed - this is the part every such integration gets wrong):
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
// would fail signature validation. public/print-agent.js reproduces the
// official qz-tray.js byte-for-byte, and this signs whatever hash it sends.

const (
	// AgentSignAlgorithm is sent to the tray as `signAlgorithm`. The tray
	// defaults to SHA1 when the field is absent (Certificate.Algorithm);
	// we always send SHA512 and sign to match.
	AgentSignAlgorithm = "SHA512"

	agentKeyFileName  = "print_agent_private_key.pem"
	agentCertFileName = "print_agent_certificate.pem"
	// The names a key made before ask 263 was saved under. That key is kept
	// for good: a new one would stop silent printing on every PC trusting it.
	legacyKeyFileName  = "qz_private_key.pem"
	legacyCertFileName = "qz_certificate.pem"
)

// agentHashPattern constrains what this server is willing to sign.
//
// The sign endpoint is, by construction, a signing oracle: an authenticated
// caller hands over a string and gets it signed by our private key. QZ Tray's
// reference backends sign whatever arrives. Restricting the input to exactly
// one SHA-256 hex digest means the oracle can only ever emit signatures over
// 32-byte hash-shaped values - it cannot be talked into signing an
// attacker-chosen document, which is the whole risk of a blind signer.
var agentHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type agentKeyMaterial struct {
	key     *rsa.PrivateKey
	certPEM string
	err     error
}

var (
	agentKeyOnce  sync.Once
	agentMaterial agentKeyMaterial
)

// loadAgentKeyMaterial resolves the signing key exactly once.
//
// Mirrors loadOrGenerateChannelCredentialKey (engines/channel_credentials.go):
// explicit env wins for production, otherwise a keypair is generated once and
// persisted outside the repo under the OS per-user config dir so it is stable
// across restarts. Unlike that function this never log.Fatalf's - printing is
// an optional convenience, and a server that cannot sign print jobs must
// still serve every other module.
func loadAgentKeyMaterial() agentKeyMaterial {
	agentKeyOnce.Do(func() {
		// QZ_*: the names a host set before ask 263, still read.
		keyPath := firstNonEmpty(os.Getenv("PRINT_AGENT_PRIVATE_KEY_PATH"), os.Getenv("QZ_PRIVATE_KEY_PATH"))
		certPath := firstNonEmpty(os.Getenv("PRINT_AGENT_CERTIFICATE_PATH"), os.Getenv("QZ_CERTIFICATE_PATH"))

		if keyPath != "" && certPath != "" {
			agentMaterial = readAgentKeyPair(keyPath, certPath)
			return
		}

		configDir, err := os.UserConfigDir()
		if err != nil {
			agentMaterial = agentKeyMaterial{err: fmt.Errorf("cannot determine user config dir for the print signing key: %w", err)}
			return
		}
		dir := filepath.Join(configDir, "custom_erp")
		for _, names := range [][2]string{{agentKeyFileName, agentCertFileName}, {legacyKeyFileName, legacyCertFileName}} {
			if m := readAgentKeyPair(filepath.Join(dir, names[0]), filepath.Join(dir, names[1])); m.err == nil {
				agentMaterial = m
				return
			}
		}

		agentMaterial = generateAgentKeyPair(dir, filepath.Join(dir, agentKeyFileName), filepath.Join(dir, agentCertFileName))
	})
	return agentMaterial
}

func readAgentKeyPair(keyPath, certPath string) agentKeyMaterial {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not read the print signing key %s: %w", keyPath, err)}
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not read the print certificate %s: %w", certPath, err)}
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return agentKeyMaterial{err: fmt.Errorf("print signing key %s is not valid PEM", keyPath)}
	}
	key, err := parseRSAPrivateKey(block.Bytes)
	if err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not parse the print signing key %s: %w", keyPath, err)}
	}
	return agentKeyMaterial{key: key, certPEM: string(certPEM)}
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
		return nil, fmt.Errorf("the print signing key must be RSA, got %T", parsed)
	}
	return key, nil
}

// generateAgentKeyPair mints a self-signed 2048-bit RSA certificate.
//
// The QZ Tray protocol requires x509 / 2048-bit RSA for an override
// certificate. A PC running QZ Tray (a marketplace PC) gets it copied in as
// override.crt - see
// docs/guides/silent-printing-setup.md. Regenerating it invalidates every
// PC's trust, so the file is only written when absent.
func generateAgentKeyPair(dir, keyPath, certPath string) agentKeyMaterial {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not generate the print signing key: %w", err)}
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not generate the print certificate serial: %w", err)}
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Custom ERP Print Authority",
			Organization: []string{"Custom ERP"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		// The agent checks the certificate's date range on every signed call. Ten
		// years keeps this off the operations backlog; rotating early just
		// means redistributing override.crt.
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not create the print certificate: %w", err)}
	}

	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := os.MkdirAll(dir, 0700); err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not create config dir for the print signing key: %w", err)}
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not persist the print signing key: %w", err)}
	}
	if err := os.WriteFile(certPath, []byte(certPEM), 0644); err != nil {
		return agentKeyMaterial{err: fmt.Errorf("could not persist the print certificate: %w", err)}
	}

	return agentKeyMaterial{key: key, certPEM: certPEM}
}

// AgentCertificatePEM returns the public certificate the browser hands to the
// print agent during the WebSocket handshake. Public by nature - it is the
// counterpart of the key, and is also what ships to each PC as override.crt.
func AgentCertificatePEM() (string, error) {
	m := loadAgentKeyMaterial()
	if m.err != nil {
		return "", m.err
	}
	return m.certPEM, nil
}

// AgentSignRequest signs one SHA-256 hex digest for the browser.
//
// hashHex is produced by public/print-agent.js as
// sha256Hex(JSON.stringify({call, params, timestamp})) - see the signing
// contract at the top of this file. The returned signature is base64, which
// is what the tray's Base64.decodeBase64 expects.
func AgentSignRequest(hashHex string) (string, error) {
	hashHex = strings.TrimSpace(strings.ToLower(hashHex))
	if !agentHashPattern.MatchString(hashHex) {
		return "", &ValidationError{
			Code:    "DEVICE-0299",
			Message: "print signing request must be a single SHA-256 hex digest",
		}
	}

	m := loadAgentKeyMaterial()
	if m.err != nil {
		return "", m.err
	}

	// Sign the UTF-8 bytes of the hex string itself, NOT the 32 raw digest
	// bytes it represents - Certificate.java hashes to hex and calls
	// getBytesUtf8 on the result before verifying.
	digest := sha512.Sum512([]byte(hashHex))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA512, digest[:])
	if err != nil {
		return "", fmt.Errorf("could not sign the print request: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// AgentPrinter is one row of the Printer Master, narrowed to what the print
// pipeline needs. Reuses the existing doctype (extended in
// db/migrations/migrations_stage31_1_qz_print.sql) rather than a parallel registry, so
// printers stay manageable from the normal Master Definition screens.
type AgentPrinter struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	OSPrinterName string `json:"os_printer_name"`
	PrintRole     string `json:"print_role"`
	Language      string `json:"printer_language"`
	Location      string `json:"location"`
	WidthMM       string `json:"label_width_mm"`
	HeightMM      string `json:"label_height_mm"`
	DPI           string `json:"dpi"`
	// Stage 137: the sticker preset - the roll's template and this
	// printer's own alignment, applied by the sticker engine.
	StickerTemplate string `json:"sticker_template"`
	ShiftRightMM    string `json:"shift_right_mm"`
	ShiftDownMM     string `json:"shift_down_mm"`
	Darkness        string `json:"darkness"`
	PrintSpeed      string `json:"print_speed"`
	// Stage 154.5: degrees to slant a print back on a crooked roll.
	SkewDeg string `json:"skew_deg"`
	// 251.2: the rolls (rollKey in public/print-check.js, comma separated)
	// whose print the user called perfect on this printer, so the ERP asks
	// how a print came out only once per printer and roll.
	AlignConfirmed string `json:"align_confirmed"`
}

// MarshalJSON adds qz_printer_name beside os_printer_name: a page loaded
// before ask 263 (a till left open over a release) reads the name there.
func (p AgentPrinter) MarshalJSON() ([]byte, error) {
	type plain AgentPrinter
	return json.Marshal(struct {
		plain
		Legacy string `json:"qz_printer_name"`
	}{plain(p), p.OSPrinterName})
}

// agentPrinterNameSQL is a Printer's OS printer name. qz_printer_name is
// where a Printer saved before ask 263 keeps it until the client runs the
// Printer names heal (engines/data_healer_printer_name.go).
const agentPrinterNameSQL = `COALESCE(NULLIF(data->>'os_printer_name', ''), data->>'qz_printer_name', '')`

// ListAgentPrinters returns every Active Printer for the tenant, so the browser
// can resolve a print role (Shipping Label / Invoice / ...) to an OS printer
// name without asking the operator to pick one per job.
func ListAgentPrinters(tenantID string) ([]AgentPrinter, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id,
		       COALESCE(data->>'code', ''),
		       COALESCE(data->>'name', ''),
		       `+agentPrinterNameSQL+`,
		       COALESCE(data->>'print_role', ''),
		       COALESCE(data->>'printer_language', ''),
		       COALESCE(data->>'location', ''),
		       COALESCE(data->>'label_width_mm', ''),
		       COALESCE(data->>'label_height_mm', ''),
		       COALESCE(data->>'dpi', ''),
		       COALESCE(data->>'sticker_template', ''),
		       COALESCE(data->>'shift_right_mm', ''),
		       COALESCE(data->>'shift_down_mm', ''),
		       COALESCE(data->>'darkness', ''),
		       COALESCE(data->>'print_speed', ''),
		       COALESCE(data->>'skew_deg', ''),
		       COALESCE(data->>'align_confirmed', '')
		FROM %s.documents
		WHERE doctype = 'Printer' AND deleted_at IS NULL
		  AND COALESCE(data->>'status', 'Active') = 'Active'
		ORDER BY COALESCE(data->>'print_role', ''), COALESCE(data->>'code', '')`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	printers := []AgentPrinter{}
	for rows.Next() {
		var p AgentPrinter
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.OSPrinterName, &p.PrintRole,
			&p.Language, &p.Location, &p.WidthMM, &p.HeightMM, &p.DPI,
			&p.StickerTemplate, &p.ShiftRightMM, &p.ShiftDownMM, &p.Darkness, &p.PrintSpeed, &p.SkewDeg, &p.AlignConfirmed); err != nil {
			return nil, err
		}
		printers = append(printers, p)
	}
	return printers, rows.Err()
}

// LogPrintJob records one submitted (or failed) print agent job.
//
// Best-effort by design: a logging failure must never lose the operator the
// label they just printed, so the error is returned for the caller to note
// but the print itself is already away.
func LogPrintJob(tenantID, jobType, documentRef, printerCode, osPrinterName, format string,
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
		  (job_type, document_ref, printer_code, os_printer_name, print_format, copies, printed_by, status, error_detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''))`, schema),
		jobType, documentRef, printerCode, osPrinterName, format, copies, printedBy, status, errorDetail)
	return err
}

// PrintJobLogEntry is one row of the print audit trail.
type PrintJobLogEntry struct {
	JobType       string    `json:"job_type"`
	DocumentRef   string    `json:"document_ref"`
	PrinterCode   string    `json:"printer_code"`
	OSPrinterName string    `json:"os_printer_name"`
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
		       COALESCE(os_printer_name, qz_printer_name, ''), COALESCE(print_format, ''), copies,
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
		if err := rows.Scan(&e.JobType, &e.DocumentRef, &e.PrinterCode, &e.OSPrinterName,
			&e.Format, &e.Copies, &e.PrintedBy, &e.Status, &e.ErrorDetail, &e.PrintedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
