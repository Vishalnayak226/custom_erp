---
doc_id: DOC-INSIDER0001
title: Insider and red-team scenarios mapped to their tests
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.18.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-28
review_by: 2026-11-28
supersedes: none
superseded_by: none
---

# Insider and red-team scenarios mapped to their tests

**Stage 49.10.6** — established 2026-10-28. Each scenario the item names is
listed below with the automated tests that prove its control, run on every
`go test`, and an explicit **Not covered** line for what no test proves yet.
`TestStage49106InsiderScenarioMapIsHonest` keeps this page honest: every test
named here must exist, and every scenario must name a test or say what is not
covered. Personas are the ones in [threat_model.md](threat_model.md).

Most tests need the test database (`TEST_DATABASE_URL`). None of the tests
below carries a build tag.

## 1. Cashier payroll access

- `TestCashierHoldsNoRestrictedCapability` (internal/server/authorization_contract_test.go) — a Cashier is refused every route classed audit log, system log, financial statement or payroll, the payroll export included.
- `TestCrossLocationAndSensitiveFieldsAreDeniedOverHTTP` (same file) — over HTTP a Cashier sees no colleague's payslip and no pay field.
- `TestPayrollFieldsAreSuperAdminOnly` (engines/sensitive_fields_test.go) — payslip, salary, loan and bank fields are neither readable nor writable by floor roles.
- `TestStage4937FloorRolesHoldNoDangerousGrant` (engines/privilege_baseline_stage49_3_7_test.go) — the Cashier, Picker and Employee Self-Service templates, and the stored Cashier role, hold no payroll, posting, account-takeover or connector-secret grant.

**Not covered:** none for the scenario as written.

## 2. Admin cross-tenant action

- `TestCrossTenantIsolationAndTokenSecurity` (internal/server/server_test.go) — a client's token with a header naming another client reads 404, not the other client's record.
- `TestStage49103TwoHostileTenantsHarness` (internal/server/stage49_10_3_tenant_harness_test.go) — two clients with colliding record ids, SKUs, names, attachments and job ids; every baseline role and the integration token of one probes reads, searches, counts, a report, attachments, record metadata, jobs and the audit log of the other, and its administrator's writes and deletes aimed at the other client change nothing there.
- `TestStage49TokenForAnUnregisteredClientIsRefused` (internal/server/stage49_3_3_tenant_test.go) — a token for a purged client is refused rather than falling back to the default client.

**Not covered:** response-timing differences between "exists in the other client" and "exists nowhere" are not measured (only status and body are compared).

## 3. DBA audit tamper

- `TestAuditDirectTamperIsDetected` (engines/audit_evidence_test.go) — an audit row edited by SQL is reported tampered.
- `TestAuditDeletedRowIsDetectedByCheckpoint` (same file) — a row deleted inside a sealed checkpoint is reported.
- `TestStage4936RoleGrantChangeEvidenceSessionsAndRollback` (engines/privilege_change_stage49_3_6_test.go) — a role change's before/after evidence edited by SQL no longer matches the digest in the signed audit chain.

**Not covered:** none for the scenario as written. A superuser who also holds the audit signing key (`AUDIT_SIGNING_KEY`, kept outside the database) could re-sign rows; keeping the two apart is a key-custody control, not a test.

## 4. Support impersonation

- `TestStage125OpenClientAsSupportUser` (internal/server/stage125_platform_support_test.go) — only a platform operator opens a client; the hand-off code is single-use and expires; no password signs in as the support user; a client admin cannot act on that user.
- `TestStage49106SupportSessionLeavesEvidence` (internal/server/stage49_10_6_insider_test.go) — issuing and redeeming the hand-off each write a row to the client's own audit log naming the operator.

**Not covered:** a time limit on the support session itself (it lasts as long as an ordinary session).

## 5. Malicious import or attachment

- `TestSanitizeCSVCell` (engines/import_sanitize_test.go) — imported formulas are neutralised.
- `TestStage49ExportCSVNeverCarriesAFormula` (engines/csv_safety_stage49_test.go) — exported CSV never carries a live formula.
- `TestAttachmentTypeIsSniffedFromBytes` (engines/stage73_attachments_test.go) — renamed text, HTML, a fake JPEG and an executable are refused; a path-traversal file name is cleaned.
- `TestStage134UploadPolicy` (engines/stage134_upload_test.go) — HTML or NUL bytes in a CSV, zip path traversal, zip bombs and pictures carrying a script are refused.

**Not covered:** none for the scenario as written.

## 6. Price, refund and threshold fraud

- `TestStage472ClientSubmittedPricesNeverReachTheSale` (internal/server/pos_pricing_stage47_2_test.go) — a price or cost sent by the till is ignored; the master price is used.
- `TestStage472OverrideBeyondThresholdRoutesToApproval` (same file) — an override above the rule is refused and waits for approval.
- `TestStage474ReturnPricesComeFromTheOriginalSale` (internal/server/returns_stage47_4_test.go) — a refund is priced from the original sale, not the request.
- `TestStage474RefundCannotBeProcessedTwice` (same file) — concurrent refunds pay out once.
- `TestStage474ExceptionPathsAreSupervisorOnlyAndEvidenced` (same file) — a no-receipt return is a supervisor's, with a reason and the approver recorded.
- `TestBLD017RefundNeverExceedsPaid` (engines/bld017_economic_properties_test.go) — randomised: refunds never exceed what was paid.
- `TestStage84SelfApprovalIsCoded` (engines/approval_self_decision_stage84_test.go) — with self-approval set to nobody, a submitter cannot decide their own voucher.

**Not covered:** threshold splitting (two bills or adjustments each under an approval limit) has no detector or test. By default (`TestStage92SubmitterMayDecideOwn`, a deliberate small-business setting) a submitter may approve their own purchase order or voucher; a client that needs the control turns self-approval off.

## 7. Owner crossover

- `TestStage59MixedOwnerIsolation` (engines/wms_owner_isolation_test.go) — two owners in one warehouse: receive, allocate, pick, pack, ship, move and count never use one owner's units for the other.

**Not covered:** none for the scenario as written.

## 8. Connector SSRF and secret theft

- `TestStage134OutboundGuard` (engines/stage134_outbound_test.go) — the outbound client every connector uses refuses loopback, private, link-local and metadata addresses, a name that resolves to one, downgrade redirects and oversized answers.
- `TestBLD031ExtensionHookTargetURLRejectsPrivateResolvedHTTPS` (engines/bld031_provider_extension_contract_test.go) — a hook aimed at a private address is refused.
- `TestChannelCredentialEncryptDecryptRoundTrip` (engines/connector_test.go) — connector credentials are encrypted at rest and tamper-evident.
- `TestStage49ConnectorSecretsAreNeverReadBack` (internal/server/stage49_8_5_credentials_test.go) — no credential endpoint returns a secret, even to Super Admin.
- `TestStage49JobPayloadNeverStoresASecret` (engines/job_secret_redaction_stage49_test.go) — job payloads never store a secret.

**Not covered:** each connector's own URL handling is not tested one by one; they share the guarded client above.

## 9. Stolen backup

- `TestStage49106BackupLeavesTheBoxEncrypted` (internal/server/stage49_10_6_insider_test.go) — the backup script refuses to run without its encryption key, encrypts the database dump with AES-256, removes the plain dump, and copies only encrypted files off the box.
- `TestStage49106aMediaCopyLeavesTheBoxEncrypted` (same file) — runs the real deploy/backup.sh under bash with pg_dump, psql and curl stubbed: without the key nothing is dumped or sent, and `media_sync` alone refuses too; with it the dump and every media file (product images, expense receipts, vendor invoice copies) reach the bucket sealed with the same key and cipher, with no plain byte in them; deploy/restore_media.sh brings the files back exactly with the key and restores nothing with a wrong one.

**Not covered:** the scripts run under Git for Windows' bash in the test, not on the Linux host, and the upload is a stub - the real curl signing against the bucket is proven only by a live run (deploy/restore_drill.sh and a restore from the bucket are the operational proof). Media copied before 49.10.6a sits in the bucket as plain files until the bucket's lifecycle rule ages it out or the operator deletes it (docs/operations/backup_restore.md). Someone with root on the host holds the key in /etc/erp/erp.env and reads everything regardless.

## 10. Compromised CI artifact

- `TestNoWorkflowActionIsUnpinned` (internal/supplychain/ledger_test.go) — every CI action is pinned to a full commit.
- `TestDependencyLedgerCoversGoModules` (same file) — every Go module is in the ledger with a checksum matching go.sum.
- `TestVerifyMigrationChecksums` (db/migrate_checksum_test.go) — an applied migration whose file changed is reported.

**Not covered:** the deployed binary is not signed, and the deploy does not verify a signature or a build checksum before it runs.

## 11. Destructive operator

- `TestStage91TenantDataReset` (engines/tenant_reset_stage91_test.go) — a data reset is refused while the client is active, without a fresh backup or without a reason, and is recorded.
- `TestStage4915PurgeRefusesUntilItIsSafeAndProvesRemoval` (engines/tenant_lifecycle_test.go) — a purge is refused for a live client, inside retention, under legal hold or without a backup reference.
- `TestStage4936RoleGrantChangeEvidenceSessionsAndRollback` (engines/privilege_change_stage49_3_6_test.go) — a role change is recorded with its before and after, and can be rolled back without restoring obsolete access.

**Not covered:** a purge or reset is one operator's decision (no second person), and the backup reference a purge asks for is not checked against a real backup.
