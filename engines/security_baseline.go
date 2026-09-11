package engines

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"custom_erp/db"
)

// Stage 49.1.3 - the fail-fast production configuration validator.
//
// Before this file the server had exactly two startup security gates, added
// one at a time as a specific bug was found: db.EnforceUTF8Encoding (20.6)
// and EnforceNoDefaultAdminCredentialInProduction (24.27). Everything else
// that makes a deployment safe - an explicit signing key instead of a
// generated local file, a real connector encryption key, a proxy trust
// setting that is not spoofable, a CORS allowlist without a wildcard, TLS
// on the database link - was a README sentence and an operator's memory.
//
// 49.1.3's requirement is that a production boot with a known-insecure
// baseline is impossible, not merely discouraged. So this is one place that
// collects every such check, classifies each finding as block or warn, and
// (only when ENV=production) refuses to start on any block finding. Outside
// production the identical checks run and log, because a developer should
// see the same list before they deploy, not after.
//
// Design constraints this file deliberately obeys:
//
//   - "Errors disclose no secret" (49.1.3's own wording). A finding names the
//     variable and what is wrong with it - never the value, never a prefix of
//     the value, never its length where that would narrow a guess.
//   - No new dependency, no config file format, no framework. Checks read the
//     same os.Getenv values the rest of the codebase already reads, so there
//     is one source of configuration truth, not two.
//   - Every check is cheap and side-effect free except the seeded-credential
//     query, which is one indexed SELECT per tenant schema at startup only.
//
// The list is intended to grow. A new security-critical environment variable
// should arrive with its check here in the same change (49.9.1), and
// TestEverySecurityCriticalEnvVarIsChecked fails the build when one does not.

// Severity values. Deliberately strings rather than an enum so they land in
// logs and in the 49.1.1 surface manifest as-is with no lookup table.
const (
	BaselineBlock = "block" // production refuses to start
	BaselineWarn  = "warn"  // logged at every boot, never fatal
)

// BaselineFinding is one thing wrong with (or worth saying about) this
// process's security configuration.
type BaselineFinding struct {
	ID       string // stable identifier, quotable in a runbook or risk register
	Severity string // BaselineBlock or BaselineWarn
	Title    string // one line, safe to log
	Detail   string // what was observed - never the observed value itself
	Remedy   string // the exact operator action that clears it
}

func (f BaselineFinding) String() string {
	return fmt.Sprintf("[%s] %s %s - %s (fix: %s)", strings.ToUpper(f.Severity), f.ID, f.Title, f.Detail, f.Remedy)
}

// IsProductionEnv centralises the ENV=production test that db.go, auth.go,
// environment.go and routes.go each spell out inline today. New code should
// call this; the existing four are left alone deliberately (touching them
// buys nothing and risks a merge conflict for no behavioural change).
func IsProductionEnv() bool {
	return os.Getenv("ENV") == "production"
}

// securityCriticalEnvVars is the 49.1.3 "unknown security-critical
// configuration" list: every environment variable whose value or absence
// changes a security property of the running server. Each one must be
// covered by a check in configurationFindings below - the test in
// security_baseline_test.go enforces that, so a variable cannot be added to
// the codebase, wired into a security decision, and then quietly never
// validated.
var securityCriticalEnvVars = []string{
	"CHANNEL_CREDENTIAL_KEY",
	"CORS_ALLOWED_ORIGINS",
	"DATABASE_URL",
	"ENV",
	"ERP_DISABLE_EXTERNAL_SIDE_EFFECTS",
	"ERP_ENABLE_EXTERNAL_SIDE_EFFECTS",
	"HOST",
	"JWT_EXPIRY_HOURS",
	"JWT_SECRET",
	"SHOPIFY_WEBHOOK_SECRET",
	"TRUSTED_PROXY_CIDRS",
	"TRUST_PROXY",
}

// SecurityCriticalEnvVars returns a copy for the 49.1.1 surface inventory and
// the baseline test.
func SecurityCriticalEnvVars() []string {
	out := make([]string, len(securityCriticalEnvVars))
	copy(out, securityCriticalEnvVars)
	return out
}

// configurationFindings runs every environment/filesystem check. It never
// touches the database, so it is safe to call before db.InitDB and safe to
// call from a test with t.Setenv.
func configurationFindings() []BaselineFinding {
	var out []BaselineFinding
	add := func(f BaselineFinding) { out = append(out, f) }
	prod := IsProductionEnv()

	// --- ENV itself -------------------------------------------------------
	switch os.Getenv("ENV") {
	case "production", "staging", "test", "development", "":
		// "" is the historical default meaning development; environment.go
		// already prints it that way in the startup banner.
	default:
		add(BaselineFinding{
			ID: "SB-001", Severity: BaselineWarn,
			Title:  "ENV holds an unrecognised value",
			Detail: "ENV is set to a value this build does not know, so every ENV-driven gate (external side effects, seed-credential enforcement, the debug route, this validator) silently takes its non-production branch",
			Remedy: "set ENV to one of production, staging, test, development - or leave it unset for development",
		})
	}

	// --- Token signing key ------------------------------------------------
	jwtSecret := os.Getenv("JWT_SECRET")
	keyringUsed := keyringSuffixConfigured("JWT_SECRET")
	if jwtSecret == "" && !keyringUsed {
		add(BaselineFinding{
			ID: "SB-002", Severity: blockInProd(prod),
			Title:  "no explicit session signing key",
			Detail: "neither JWT_SECRET nor a JWT_SECRET_<n> keyring is set, so engines/auth.go generates one and persists it under the OS user config dir - a key nobody chose, that no second instance shares, that no backup captures, and that cannot be rotated without logging everyone out",
			Remedy: "set JWT_SECRET (or the JWT_SECRET_<n> rotation keyring) to a high-entropy value from the deployment's secret store",
		})
	}
	if jwtSecret != "" && len(jwtSecret) < 32 {
		add(BaselineFinding{
			ID: "SB-003", Severity: blockInProd(prod),
			Title:  "session signing key is too short",
			Detail: "JWT_SECRET is shorter than the 32-byte HMAC-SHA256 block the signer uses, which is below the entropy this build's own generated fallback produces",
			Remedy: "replace JWT_SECRET with at least 32 bytes of CSPRNG output and re-issue sessions",
		})
	}
	if jwtSecret != "" && isLowEntropyPlaceholder(jwtSecret) {
		add(BaselineFinding{
			ID: "SB-004", Severity: blockInProd(prod),
			Title:  "session signing key looks like a placeholder",
			Detail: "JWT_SECRET matches a documentation/example placeholder pattern (a single repeated character, or a word like changeme/secret/example/test), so any reader of this project's docs can forge a session token",
			Remedy: "replace JWT_SECRET with CSPRNG output and treat every session issued under the old key as compromised",
		})
	}
	if hours := os.Getenv("JWT_EXPIRY_HOURS"); hours != "" {
		if n, err := strconv.Atoi(hours); err != nil || n <= 0 {
			add(BaselineFinding{
				ID: "SB-005", Severity: BaselineWarn,
				Title:  "JWT_EXPIRY_HOURS is not a positive integer",
				Detail: "the value cannot be parsed, so it is ignored and the per-tenant security.session_token_ttl_hours setting silently applies instead of the value the operator believes is in force",
				Remedy: "set JWT_EXPIRY_HOURS to a positive whole number of hours, or unset it to use the tenant setting",
			})
		} else if n > 24 {
			add(BaselineFinding{
				ID: "SB-006", Severity: BaselineWarn,
				Title:  "session lifetime exceeds 24 hours",
				Detail: "JWT_EXPIRY_HOURS grants sessions a lifetime longer than a day; a stolen token stays usable for that whole window, and this build has no server-side token revocation list to shorten it",
				Remedy: "reduce JWT_EXPIRY_HOURS to 24 or less (8-12 is typical for a staffed shift)",
			})
		}
	}

	// --- Connector credential encryption key ------------------------------
	//
	// Warn, not block. A deployment with no sales-channel connector stores no
	// ciphertext, so the generated local key protects nothing and refusing to
	// start would be a control operators are entitled to resent. The blocking
	// version is SB-022 below, which fires only once there is actually
	// something encrypted with it.
	channelKey := os.Getenv("CHANNEL_CREDENTIAL_KEY")
	channelKeyringUsed := keyringSuffixConfigured("CHANNEL_CREDENTIAL_KEY")
	if channelKey == "" && !channelKeyringUsed {
		add(BaselineFinding{
			ID: "SB-007", Severity: BaselineWarn,
			Title:  "no explicit connector credential encryption key",
			Detail: "CHANNEL_CREDENTIAL_KEY is unset, so the AES-256-GCM key protecting stored Shopify/BigCommerce/Magento tokens would be generated into the OS user config dir - a key no backup captures and no other host can reproduce",
			Remedy: "set CHANNEL_CREDENTIAL_KEY (or the CHANNEL_CREDENTIAL_KEY_<n> rotation keyring) to exactly 32 bytes before saving any connector credential, and record it in the key inventory (49.6.5)",
		})
	}
	if channelKey != "" && isLowEntropyPlaceholder(channelKey) {
		add(BaselineFinding{
			ID: "SB-023", Severity: blockInProd(prod),
			Title:  "connector credential encryption key looks like a placeholder",
			Detail: "CHANNEL_CREDENTIAL_KEY matches a documentation/example placeholder pattern (a single repeated character, or a word like changeme/secret/example/test), so any reader of this project's docs can decrypt stored connector credentials",
			Remedy: "replace CHANNEL_CREDENTIAL_KEY with 32 bytes of CSPRNG output and re-encrypt stored credentials (tenantctl reencrypt-channel-credentials) under the new key",
		})
	}

	// --- Cross-origin policy ---------------------------------------------
	for _, o := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" || o == "null" {
			add(BaselineFinding{
				ID: "SB-008", Severity: BaselineBlock,
				Title:  "CORS allowlist contains a wildcard or null origin",
				Detail: "an entry in CORS_ALLOWED_ORIGINS is a wildcard or the null origin, which lets any website - including a sandboxed iframe or a local file - read authenticated API responses from a victim's browser",
				Remedy: "list each permitted origin by exact scheme, host and port",
			})
			continue
		}
		u, err := url.Parse(o)
		if err != nil || u.Scheme == "" || u.Host == "" {
			add(BaselineFinding{
				ID: "SB-009", Severity: BaselineWarn,
				Title:  "CORS allowlist entry is not a valid origin",
				Detail: "an entry in CORS_ALLOWED_ORIGINS is not a parseable scheme://host[:port] origin, so it can never match a browser's Origin header and is silently dead configuration",
				Remedy: "write each entry as scheme://host[:port] with no path or trailing slash",
			})
			continue
		}
		if u.Path != "" && u.Path != "/" {
			add(BaselineFinding{
				ID: "SB-010", Severity: BaselineWarn,
				Title:  "CORS allowlist entry carries a path",
				Detail: "a browser's Origin header never contains a path, so an entry with one can never match and is dead configuration",
				Remedy: "strip the path from the entry, leaving scheme://host[:port]",
			})
		}
		if prod && u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
			add(BaselineFinding{
				ID: "SB-011", Severity: BaselineBlock,
				Title:  "non-TLS origin in the production CORS allowlist",
				Detail: "a non-loopback http:// origin is permitted to make credentialed cross-origin calls, so a network attacker who can serve that origin can read authenticated responses",
				Remedy: "use the https:// form of the origin, or remove it",
			})
		}
	}

	// --- Proxy trust ------------------------------------------------------
	trustProxy := os.Getenv("TRUST_PROXY") == "1" || strings.EqualFold(os.Getenv("TRUST_PROXY"), "true")
	cidrs := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if trustProxy && cidrs == "" {
		add(BaselineFinding{
			ID: "SB-012", Severity: blockInProd(prod),
			Title:  "forwarded client IPs are trusted from any peer",
			Detail: "TRUST_PROXY is on but TRUSTED_PROXY_CIDRS is empty, so any caller that can reach the process can set its own client IP through a forwarding header - which forges the identity that login rate limiting, lockout and the audit trail all record",
			Remedy: "set TRUSTED_PROXY_CIDRS to the reverse proxy's address range only (e.g. 127.0.0.1/32), or turn TRUST_PROXY off",
		})
	}
	for _, c := range strings.Split(cidrs, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(c); err != nil && net.ParseIP(c) == nil {
			add(BaselineFinding{
				ID: "SB-013", Severity: BaselineWarn,
				Title:  "TRUSTED_PROXY_CIDRS entry is not a valid address or CIDR",
				Detail: "an entry cannot be parsed as an IP address or CIDR block, so it never matches and the proxy it was meant to describe is not actually trusted",
				Remedy: "write each entry as an IP address or a CIDR block, comma separated",
			})
		}
	}
	if prod && !trustProxy && os.Getenv("HOST") == "" {
		add(BaselineFinding{
			ID: "SB-014", Severity: BaselineWarn,
			Title:  "listener is bound to every interface with no proxy trust configured",
			Detail: "HOST is unset so the server listens on all interfaces, and TRUST_PROXY is off so it is not expecting a reverse proxy either - if anything can route to this port directly it reaches the application without the proxy's TLS and access controls",
			Remedy: "set HOST=127.0.0.1 and terminate TLS in the reverse proxy, or record the direct-exposure decision in the deployment record",
		})
	}

	// --- Database link ----------------------------------------------------
	if conn := os.Getenv("DATABASE_URL"); conn != "" {
		if host, ssl, ok := parsePostgresConnString(conn); ok {
			if prod && !isLoopbackHost(host) && (ssl == "disable" || ssl == "allow" || ssl == "") {
				add(BaselineFinding{
					ID: "SB-015", Severity: BaselineBlock,
					Title:  "database connection to a remote host is not required to use TLS",
					Detail: "DATABASE_URL points at a non-loopback host with an sslmode that permits an unencrypted connection, so every row this server reads or writes - including password hashes and connector ciphertext - can cross the network in clear text",
					Remedy: "set sslmode=verify-full (or at minimum require) in DATABASE_URL and give the server the CA it should verify against",
				})
			}
		}
	} else if prod {
		add(BaselineFinding{
			ID: "SB-016", Severity: BaselineWarn,
			Title:  "DATABASE_URL is unset in production",
			Detail: "the built-in development fallback connection string is in use (local postgres, sslmode=disable, no password), which is a development convenience and not a deployment configuration",
			Remedy: "set DATABASE_URL explicitly for this deployment",
		})
	}

	// --- External side effects -------------------------------------------
	if prod && os.Getenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS") == "1" {
		add(BaselineFinding{
			ID: "SB-017", Severity: BaselineWarn,
			Title:  "the external side-effect kill switch is engaged in production",
			Detail: "ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1 means no webhook, email or ops alert will actually leave this instance - correct during an incident, and silent loss of every notification if it was left set afterwards",
			Remedy: "unset it once the incident that engaged it is closed, and record the window in the incident log",
		})
	}
	if prod && os.Getenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS") == "1" {
		add(BaselineFinding{
			ID: "SB-018", Severity: BaselineWarn,
			Title:  "redundant external side-effect opt-in in production",
			Detail: "ERP_ENABLE_EXTERNAL_SIDE_EFFECTS only has an effect outside production; its presence here usually means this environment file was copied from a staging host, which is worth confirming before trusting the rest of it",
			Remedy: "remove the variable from the production environment file",
		})
	}
	if prod && os.Getenv("SHOPIFY_WEBHOOK_SECRET") == "" {
		add(BaselineFinding{
			ID: "SB-019", Severity: BaselineWarn,
			Title:  "inbound Shopify webhooks are rejected",
			Detail: "SHOPIFY_WEBHOOK_SECRET is unset, so the signature check fails closed and every inbound Shopify webhook is refused - safe, but silently non-functional if a Shopify channel is live",
			Remedy: "set SHOPIFY_WEBHOOK_SECRET if a Shopify channel is connected; ignore this finding if none is",
		})
	}

	// --- Secret file permissions -----------------------------------------
	out = append(out, secretFilePermissionFindings()...)

	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func blockInProd(prod bool) string {
	if prod {
		return BaselineBlock
	}
	return BaselineWarn
}

// secretFilePermissionFindings checks the two keys this build generates on
// disk when the matching environment variable is absent. POSIX only -
// Windows reports synthetic mode bits that mean nothing, and the dev machine
// this project is built on is Windows, so checking there would produce a
// permanent false finding.
func secretFilePermissionFindings() []BaselineFinding {
	if runtime.GOOS == "windows" {
		return nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	var out []BaselineFinding
	for _, f := range []struct{ file, what string }{
		{"jwt_secret.local", "the session signing key"},
		{"channel_cred_key.local", "the connector credential encryption key"},
	} {
		info, err := os.Stat(filepath.Join(configDir, "custom_erp", f.file))
		if err != nil {
			continue // not generated on this host - the env var is set, which is the good case
		}
		if info.Mode().Perm()&0o077 != 0 {
			out = append(out, BaselineFinding{
				ID: "SB-020", Severity: BaselineBlock,
				Title:  "a generated key file is readable by other local users",
				Detail: fmt.Sprintf("the file holding %s has group or world permission bits set, so any other account on this host can read it", f.what),
				Remedy: "chmod 600 the file and confirm no other service account shares this Unix user (49.7.1)",
			})
		}
	}
	return out
}

// --- Seeded credentials ---------------------------------------------------

// seededCredentialHashes are the exact bcrypt hashes db/migration.sql ships
// for its four bootstrap accounts. 24.27 already guarded the first of them,
// in tenant_default only; 49.1.3 widens that to every seeded account in every
// tenant schema, because three of the four were never checked at all - and
// one of them ("system") carries the HR/Admin role, which IsSuperAdmin treats
// as unrestricted.
//
// TestSeededCredentialHashesCoverMigrationSQL keeps this list honest: it
// reads db/migration.sql and fails if a password hash is seeded there that is
// not listed here, so a fifth bootstrap account cannot be added without also
// being guarded.
var seededCredentialHashes = map[string]string{
	"admin":    seedAdminPasswordHash,
	"cashier1": "$2a$10$u2OOnj/nClI2tPmLfyTPpuePXesLvp1oOwzfK4EAmKFNxbYeJzS5u",
	"manager1": "$2a$10$fHhJ.2w4FG65vw.GNGYn3erEqsCrXUmuI3loj1lJIH58fCVW7gfli",
	"system":   "$2a$10$pGKA1HuK0gtwNaDkE5a25eOzmPgz9cobEJIHeL2RU1e2x7iwema8W",
}

// seededCredentialFindings reports every tenant schema still carrying one of
// the shipped bootstrap password hashes on an account that can still log in.
// One SELECT per schema, at startup only. A database error is never turned
// into a finding - an unreachable database has its own, louder failure path
// immediately above this one in Run().
func seededCredentialFindings() []BaselineFinding {
	if db.DB == nil {
		return nil
	}
	schemas, err := listTenantSchemas()
	if err != nil {
		return nil
	}
	var names []string
	for _, schema := range schemas {
		// 49.3.3: a schema name reaches SQL as an identifier, never as a
		// parameter, so it is validated against the shape the provisioner
		// actually produces before it is interpolated. Every other schema
		// fan-out in this package interpolates the registry value directly;
		// this is the safer form new code should copy.
		if !validSchemaIdent(schema) {
			continue
		}
		rows, err := db.DB.Query(fmt.Sprintf(
			`SELECT username, password_hash FROM %s.users WHERE status <> 'Inactive'`, schema))
		if err != nil {
			continue
		}
		for rows.Next() {
			var username, hash string
			if err := rows.Scan(&username, &hash); err != nil {
				continue
			}
			for _, seeded := range seededCredentialHashes {
				if hash == seeded {
					names = append(names, schema+"."+username)
					break
				}
			}
		}
		rows.Close()
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return []BaselineFinding{{
		ID: "SB-021", Severity: blockInProd(IsProductionEnv()),
		Title: "a shipped bootstrap password is still active",
		Detail: fmt.Sprintf(
			"%d active account(s) still carry a password hash that db/migration.sql ships in this repository, so anyone holding the source holds the password: %s",
			len(names), strings.Join(names, ", ")),
		Remedy: "rotate each account's password (or set its status to Inactive); note that re-running db/migration.sql resets them again, because its users INSERT ends in ON CONFLICT DO UPDATE SET password_hash = EXCLUDED.password_hash",
	}}
}

// storedConnectorCredentialFindings is the blocking half of SB-007: the
// generated-on-disk connector key only matters once something is actually
// encrypted with it. Once a channel_credentials row exists, that key is the
// only thing standing between a database backup and a live Shopify/Magento
// API token - and it lives in one host's user config directory, outside every
// backup this deployment takes.
func storedConnectorCredentialFindings() []BaselineFinding {
	if db.DB == nil || os.Getenv("CHANNEL_CREDENTIAL_KEY") != "" || keyringSuffixConfigured("CHANNEL_CREDENTIAL_KEY") {
		return nil
	}
	schemas, err := listTenantSchemas()
	if err != nil {
		return nil
	}
	total := 0
	for _, schema := range schemas {
		if !validSchemaIdent(schema) {
			continue
		}
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.channel_credentials`, schema)).Scan(&n); err != nil {
			continue // table absent on this schema - nothing stored, nothing to protect
		}
		total += n
	}
	if total == 0 {
		return nil
	}
	return []BaselineFinding{{
		ID: "SB-022", Severity: blockInProd(IsProductionEnv()),
		Title: "stored connector credentials are protected by a key that is not part of this deployment",
		Detail: fmt.Sprintf(
			"%d stored channel credential(s) are encrypted with a key generated into this host's user config directory because CHANNEL_CREDENTIAL_KEY is unset; the database backup holds the ciphertext but not the key, so a restore onto any other host silently cannot decrypt them - and losing this host loses the credentials",
			total),
		Remedy: "set CHANNEL_CREDENTIAL_KEY to the existing key's value (it is in this host's user config dir as channel_cred_key.local) before the next restart, then plan a re-encryption under a managed key",
	}}
}

// migrationChecksumFindings is the 49.7.5 half of the migration-safety item:
// db.VerifyMigrationChecksums reports any already-applied migration file
// whose content has since changed, or that has vanished from this binary.
// Both are exactly the "no hand-edited mystery state" the security
// constitution names - this codebase's migrations are meant to be additive
// and immutable once applied, never edited after the fact.
func migrationChecksumFindings() []BaselineFinding {
	if db.DB == nil {
		return nil
	}
	drift, err := db.VerifyMigrationChecksums()
	if err != nil || len(drift) == 0 {
		return nil
	}
	names := make([]string, 0, len(drift))
	for _, d := range drift {
		names = append(names, d.File)
	}
	sort.Strings(names)
	return []BaselineFinding{{
		ID: "SB-024", Severity: blockInProd(IsProductionEnv()),
		Title: "an already-applied migration file no longer matches what this database recorded",
		Detail: fmt.Sprintf(
			"%d migration file(s) changed content (or disappeared from this binary) after being applied: %s - either the file was edited after shipping, or this binary was built from a history that does not match what actually ran against this database",
			len(drift), strings.Join(names, ", ")),
		Remedy: "never edit a migration file after it has shipped - add a new additive file instead; investigate how the mismatched file's content diverged from what this database's ledger recorded before trusting this database further",
	}}
}

// --- Entry points ---------------------------------------------------------

// ProductionSecurityBaseline returns every finding for this process,
// configuration and database together. Exported so an ops/status surface and
// the 49.1.6 drift check can read the same list the startup gate acts on.
func ProductionSecurityBaseline() []BaselineFinding {
	findings := configurationFindings()
	findings = append(findings, seededCredentialFindings()...)
	findings = append(findings, storedConnectorCredentialFindings()...)
	findings = append(findings, migrationChecksumFindings()...)
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	return findings
}

// EnforceProductionSecurityBaseline is the startup gate, called from Run()
// alongside the two checks that predate it. It logs every finding at every
// ENV, and returns a non-nil error - which Run turns into a refusal to start
// - only when ENV=production and at least one finding is a block.
//
// The non-production path is a deliberate design choice, not laxity: a
// developer's box legitimately has no JWT_SECRET and a seeded admin, and
// hard-failing there would train everyone to delete the check. Printing the
// same list they will see in production, at every boot, is what makes the
// production refusal unsurprising.
func EnforceProductionSecurityBaseline() error {
	findings := ProductionSecurityBaseline()
	if len(findings) == 0 {
		log.Println("[SECURITY] production baseline: no findings - this configuration passes every 49.1.3 check")
		return nil
	}
	var blockingIDs []string
	for _, f := range findings {
		log.Printf("[SECURITY] %s", f)
		if f.Severity == BaselineBlock {
			blockingIDs = append(blockingIDs, f.ID)
		}
	}
	if !IsProductionEnv() || len(blockingIDs) == 0 {
		return nil
	}
	return fmt.Errorf("%d blocking security-baseline finding(s) for ENV=production (%s) - each is logged above with the exact fix; correct them, or start without ENV=production for a development run",
		len(blockingIDs), strings.Join(blockingIDs, ", "))
}

// --- helpers --------------------------------------------------------------

// isLowEntropyPlaceholder catches the values that appear in documentation,
// tutorials and hurried deployments. It is a smell test, not an entropy
// measure: a long random string that merely contains "test" is not flagged,
// only one whose whole value is one of these shapes.
// keyringSuffixConfigured reports whether any envName_<n> (positive integer
// suffix) environment variable is set to a non-empty value - the rotation
// keyring pattern shared by JWT_SECRET_<n> (Stage 29.8) and
// CHANNEL_CREDENTIAL_KEY_<n> (Stage 49.6.5, engines/secret_keyring.go).
func keyringSuffixConfigured(envName string) bool {
	prefix := envName + "_"
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, value := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(name, prefix) || value == "" {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(name, prefix)); err == nil {
			return true
		}
	}
	return false
}

func isLowEntropyPlaceholder(v string) bool {
	lower := strings.ToLower(strings.TrimSpace(v))
	if lower == "" {
		return false
	}
	for _, p := range []string{
		// "change_me" catches deploy/erp.env.example's own shipped value
		// (JWT_SECRET=CHANGE_ME_openssl_rand_hex_48) - the single most likely
		// way an unrotated placeholder reaches a real deployment.
		"changeme", "change-me", "change_me", "secret", "mysecret", "supersecret", "password",
		"example", "placeholder", "test", "testing", "dev", "development",
		"your-secret-here", "replace-me", "todo", "xxx",
	} {
		if lower == p || strings.HasPrefix(lower, p+"-") || strings.HasPrefix(lower, p+"_") {
			return true
		}
	}
	// A single repeated character ("aaaa...", "0000..."), whatever its length.
	if len(lower) > 1 && strings.Count(lower, lower[:1]) == len(lower) {
		return true
	}
	return false
}

// validSchemaIdent accepts only the identifier shape this system's tenant
// provisioner produces: lowercase letters, digits and underscores, starting
// with a letter. Anything else is refused rather than quoted, because a
// registry row that does not look like a schema name is itself a finding, not
// a string to escape and carry on with.
var schemaIdentPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func validSchemaIdent(s string) bool { return schemaIdentPattern.MatchString(s) }

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// parsePostgresConnString pulls the host and sslmode out of a libpq
// connection string in either URL form or key/value form - db.ConnStringFromEnv
// passes whatever the operator set straight through to lib/pq, which accepts
// both.
func parsePostgresConnString(conn string) (host, sslmode string, ok bool) {
	if strings.HasPrefix(conn, "postgres://") || strings.HasPrefix(conn, "postgresql://") {
		u, err := url.Parse(conn)
		if err != nil {
			return "", "", false
		}
		return u.Hostname(), strings.ToLower(u.Query().Get("sslmode")), true
	}
	if !strings.Contains(conn, "=") {
		return "", "", false
	}
	for _, field := range strings.Fields(conn) {
		k, v, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		switch strings.ToLower(k) {
		case "host":
			host = v
		case "sslmode":
			sslmode = strings.ToLower(v)
		}
	}
	return host, sslmode, host != ""
}
