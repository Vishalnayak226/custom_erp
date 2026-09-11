package engines

import "sort"

// Stage 49.6.1 - field/data-flow classification registry.
//
// Extends, rather than duplicates, the sensitive-field policy in
// engines/sensitive_fields.go (Stage 47.1.3): that file already names six
// categories (hr.payroll, finance.bank, hr.grievance, inventory.cost,
// integration.secret, privacy.personal) and decides who may read/write each
// field they cover. This file adds the two dimensions 49.6.1 itself asks
// for on top of that existing decision, one row per CATEGORY rather than one
// row per field - a category's purpose/masking/retention answer is shared
// by every field in it, so repeating it per field would be exactly the
// "duplicate the existing work" this item was told not to do:
//
//   - a classification TIER (public/internal/confidential/restricted),
//     independent of role-based read/write - "how sensitive is a database
//     dump or log line containing this value, on its own."
//   - a PRIVACY category, where one applies (personal/sensitive_financial/
//     authentication/audit/legal) - the DPDP/47.16-relevant tag. A field can
//     be confidential without being personal data at all (cost/margin).
//
// It also names data flows entirely outside sensitive_fields.go's scope -
// login credentials, signing/encryption keys, audit evidence, backups -
// because a classification registry that only covered document fields would
// miss most of what threat_model.md §2's crown-jewel ranking actually cares
// about. TestSensitiveFieldCategoriesAreAllClassified
// (data_classification_test.go) fails the build if sensitive_fields.go ever
// names a category this file does not carry a row for, so the two files
// cannot silently drift apart.
//
// Several Deletion/RetentionTrigger answers below are marked
// "[needs decision: ...]" rather than guessed: the exact statutory retention
// window is a legal question owned by 47.16, not something a build session
// can invent. What this file commits to instead is that every category has
// SOME entry, so "we never decided" is visible in the registry itself rather
// than being an undocumented gap.

// DataTier is the sensitivity of a value on its own, independent of who is
// currently allowed to read it.
type DataTier string

const (
	TierPublic       DataTier = "public"
	TierInternal     DataTier = "internal"
	TierConfidential DataTier = "confidential"
	TierRestricted   DataTier = "restricted"
)

// PrivacyCategory tags the DPDP/47.16-relevant flavor of a category, where
// one applies. Empty for confidential/restricted data that is not personal
// or otherwise privacy-flavored (e.g. cost/margin).
type PrivacyCategory string

const (
	PrivacyPersonal           PrivacyCategory = "personal"
	PrivacySensitiveFinancial PrivacyCategory = "sensitive_financial"
	PrivacyAuthentication     PrivacyCategory = "authentication"
	PrivacyAudit              PrivacyCategory = "audit"
	PrivacyLegal              PrivacyCategory = "legal"
)

// Flow keys for data outside any doctype field - sensitive_fields.go has no
// category constant for these because they are not document fields at all.
const (
	DataFlowAuthCredential = "auth.credential"
	DataFlowSigningKey     = "auth.signing_key"
	DataFlowAuditEvidence  = "audit.evidence"
	DataFlowBackup         = "operations.backup"
)

// DataFlowClass is one row of the 49.6.1 registry: everything an operator or
// an automated privacy-rights/retention job needs to decide what happens to
// a category of data, without re-deriving it from first principles.
type DataFlowClass struct {
	Tier    DataTier
	Privacy PrivacyCategory

	Purpose      string // why this data is collected/held at all
	TenantScoped bool   // lives in a tenant schema (region/residency follows the tenant) vs. shared/global
	Source       string // where the value originates
	Consumers    string // which roles/subsystems legitimately read it

	Masking          string // how it is masked/redacted for a non-privileged viewer or a log line
	Export           string // CSV/report export behavior
	RetentionTrigger string // the event that starts the retention clock
	LegalHold        bool   // can be placed under legal hold, blocking deletion/anonymization
	Deletion         string // what "delete" actually does to this data
}

// dataClassifications is keyed by the sensitiveFields category constants
// (SensitiveCategoryPayroll etc.) plus the DataFlow* keys above - never a
// second, parallel field-level map.
var dataClassifications = map[string]DataFlowClass{
	SensitiveCategoryPayroll: {
		Tier: TierRestricted, Privacy: PrivacySensitiveFinancial,
		Purpose: "Run statutory payroll and reconcile it against filings", TenantScoped: true,
		Source: "HR payroll run; admin-entered salary structure",
		Consumers: "Super Admin only (engines/sensitive_fields.go payrollRule)",
		Masking: "Never returned to a role outside the read list; report columns marked ReportColumn.Sensitive",
		Export: "Excluded from CSV/report export for any role outside the read list",
		RetentionTrigger: "employment end", LegalHold: true,
		Deletion: "[needs decision: statutory payroll/tax record retention years - 47.16.2/47.16.4] then anonymize rather than hard-delete so period-end GL totals still reconcile",
	},
	SensitiveCategoryBank: {
		Tier: TierRestricted, Privacy: PrivacySensitiveFinancial,
		Purpose: "Pay a vendor or an employee, or receive a company payment", TenantScoped: true,
		Source: "Admin-entered bank details",
		Consumers: "Super Admin only",
		Masking: "Never returned to a role outside the read list",
		Export: "Excluded from CSV/report export for any role outside the read list",
		RetentionTrigger: "vendor/employee relationship end", LegalHold: true,
		Deletion: "[needs decision: hold window before anonymization once no open payment references the account]",
	},
	SensitiveCategoryGrievance: {
		Tier: TierRestricted, Privacy: PrivacyPersonal,
		Purpose: "Investigate and resolve an employee's grievance", TenantScoped: true,
		Source: "Employee self-service submission",
		Consumers: "Super Admin (adjudicator); the submitting employee's own write",
		Masking: "Never returned to a role outside the read list",
		Export: "Excluded from export",
		RetentionTrigger: "case closed", LegalHold: true,
		Deletion: "[needs decision: HR retention policy for closed grievance case content]",
	},
	SensitiveCategoryCost: {
		Tier: TierConfidential, Privacy: "",
		Purpose: "Cost the sale, compute margin, value inventory", TenantScoped: true,
		Source: "Purchase/production cost capture",
		Consumers: "Super Admin, Store Manager (engines/sensitive_fields.go costRule)",
		Masking: "Hidden from Cashier in read, write and export paths",
		Export: "Excluded from CSV/report export for roles that cannot read it",
		RetentionTrigger: "none - operational data for the life of the item/order", LegalHold: false,
		Deletion: "Deleted/archived with the parent record; no independent privacy lifecycle",
	},
	SensitiveCategorySecret: {
		Tier: TierRestricted, Privacy: PrivacyAuthentication,
		Purpose: "Sign/verify webhook deliveries, or authenticate to a connector/integration", TenantScoped: true,
		Source: "System-generated (share/hook tokens) or admin-entered (connector credential, webhook secret)",
		Consumers: "The one code path that verifies it; never returned by any HTTP handler",
		Masking: "Write-only where a role must set it without reading it back (anySensitiveWriter); AES-256-GCM at rest for connector credentials (engines/channel_credentials.go, engines/secret_keyring.go)",
		Export: "Never exported, never logged",
		RetentionTrigger: "connector/integration disconnected", LegalHold: false,
		Deletion: "Hard-deleted when the connector/integration is removed - a revoked secret has no evidentiary value worth a legal hold",
	},
	SensitiveCategoryPersonal: {
		Tier: TierConfidential, Privacy: PrivacyPersonal,
		Purpose: "Identify and reach a customer for the transaction/relationship", TenantScoped: true,
		Source: "Customer-provided at POS/checkout or self-service",
		Consumers: "Operational roles for phone/email/name (POS lookup); Store Manager+ for date_of_birth",
		Masking: "date_of_birth hidden from Cashier; phone/email/name deliberately NOT restricted - operationally required at every sale",
		Export: "Subject to the same field policy as read access",
		RetentionTrigger: "consent withdrawal or account closure", LegalHold: true,
		Deletion: "Anonymize (engines/privacy_rights.go) rather than hard-delete: Sales/Invoice rows reference customer_id and must survive for statutory financial retention",
	},

	// --- Flows outside any doctype field ----------------------------------
	DataFlowAuthCredential: {
		Tier: TierRestricted, Privacy: PrivacyAuthentication,
		Purpose: "Verify a login attempt", TenantScoped: true,
		Source: "User-chosen at signup/reset",
		Consumers: "bcrypt comparison in engines/auth.go only",
		Masking: "bcrypt hash only; plaintext never stored, never logged, never returned by any handler",
		Export: "Never exported",
		RetentionTrigger: "account deactivation", LegalHold: false,
		Deletion: "Hash retained until the user row itself is anonymized/deleted; a bcrypt hash alone re-identifies nobody",
	},
	DataFlowSigningKey: {
		Tier: TierRestricted, Privacy: "",
		Purpose: "Sign session tokens / encrypt connector credentials at rest", TenantScoped: false,
		Source: "CSPRNG (crypto/rand); operator-provided, or generated once per host as a local fallback",
		Consumers: "The signing/verification code path only (engines/auth.go, engines/channel_credentials.go)",
		Masking: "Never logged - security_baseline.go findings name the variable, never its value (TestBaselineFindingsNeverEchoConfiguredValues)",
		Export: "Never exported, never captured in an application-level backup",
		RetentionTrigger: "planned rotation or suspected compromise", LegalHold: false,
		Deletion: "Old key destroyed only after every ciphertext/token it protects has been re-issued/re-encrypted under the new key (49.6.5)",
	},
	DataFlowAuditEvidence: {
		Tier: TierRestricted, Privacy: PrivacyAudit,
		Purpose: "Prove what happened, for dispute resolution and statutory evidence (47.16.3)", TenantScoped: true,
		Source: "System-generated on every audited action",
		Consumers: "Super Admin (audit log viewer); the checksum-chain verifier",
		Masking: "Detail strings must not carry secret values - see engines/telemetry_redaction.go (Stage 49.6.7)",
		Export: "Admin-only export; never bulk-exported to a non-admin role",
		RetentionTrigger: "none while the checksum-chain requirement (47.16.3) is in force", LegalHold: true,
		Deletion: "[needs decision: statutory audit-log retention period once 47.16.3 sets it - never silently pruned before then]",
	},
	DataFlowBackup: {
		Tier: TierRestricted, Privacy: "",
		Purpose: "Recover from data loss within the approved RTO", TenantScoped: false,
		Source: "Nightly pg_dump (deploy/backup.sh)",
		Consumers: "The restore procedure only",
		Masking: "Encrypted at rest with BACKUP_ENCRYPTION_KEY; contains every tier of data in the system, so its own access control is the strongest one in the deployment",
		Export: "Never exported; a restore is the only sanctioned read path",
		RetentionTrigger: "backup rotation policy", LegalHold: true,
		Deletion: "[needs decision: backup retention/rotation window, and whether a legal hold pins specific dated backups - 49.13 owns the backup lifecycle]",
	},
}

// ClassifyCategory looks up a data-flow classification by its category key -
// one of the sensitiveFields SensitiveCategory* constants, or a DataFlow*
// constant for a flow outside any doctype field.
func ClassifyCategory(category string) (DataFlowClass, bool) {
	c, ok := dataClassifications[category]
	return c, ok
}

// ClassifyDoctypeField resolves a document field's classification by first
// finding its category in sensitive_fields.go's rule table, then looking
// that category up here. Returns ok=false for a field sensitive_fields.go
// does not cover (which is not itself a finding - most fields are
// intentionally uncovered, e.g. Customer.phone).
func ClassifyDoctypeField(doctype, field string) (DataFlowClass, bool) {
	rules, ok := sensitiveFields[doctype]
	if !ok {
		return DataFlowClass{}, false
	}
	rule, ok := rules[field]
	if !ok {
		return DataFlowClass{}, false
	}
	return ClassifyCategory(rule.Category)
}

// AllDataFlowCategories lists every category this registry classifies,
// sorted - for an admin-facing inventory view and for the drift-guard test.
func AllDataFlowCategories() []string {
	names := make([]string, 0, len(dataClassifications))
	for k := range dataClassifications {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
