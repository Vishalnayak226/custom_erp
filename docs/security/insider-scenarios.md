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
covered. Personas are the ones in [threat-model.md](threat-model.md).

Most tests need the test database (`TEST_DATABASE_URL`). None of the tests
below carries a build tag.

## 1. Cashier payroll access

- `TestCashierHoldsNoRestrictedCapability` (internal/server/authorization_contract_test.go) — a Cashier is refused every route classed audit log, system log, financial statement or payroll, the payroll export included.
- `TestCrossLocationAndSensitiveFieldsAreDeniedOverHTTP` (same file) — over HTTP a Cashier sees no colleague's payslip and no pay field.
- `TestPayrollFieldsAreSuperAdminOnly` (engines/sensitive_fields_test.go) — payslip, salary, loan and bank fields are neither readable nor writable by floor roles.
- `TestStage4937FloorRolesHoldNoDangerousGrant` (engines/privilege_baseline_test.go) — the Cashier, Picker and Employee Self-Service templates, and the stored Cashier role, hold no payroll, posting, account-takeover or connector-secret grant.

**Not covered:** none for the scenario as written.

## 2. Admin cross-tenant action

- `TestCrossTenantIsolationAndTokenSecurity` (internal/server/server_test.go) — a client's token with a header naming another client reads 404, not the other client's record.
- `TestStage49103TwoHostileTenantsHarness` (internal/server/tenant_harness_test.go) — two clients with colliding record ids, SKUs, names, attachments and job ids; every baseline role and the integration token of one probes reads, searches, counts, a report, attachments, record metadata, jobs and the audit log of the other, and its administrator's writes and deletes aimed at the other client change nothing there.
- `TestStage49TokenForAnUnregisteredClientIsRefused` (internal/server/tenant_test.go) — a token for a purged client is refused rather than falling back to the default client.

**Not covered:** response-timing differences between "exists in the other client" and "exists nowhere" are not measured (only status and body are compared).

## 3. DBA audit tamper

- `TestAuditDirectTamperIsDetected` (engines/audit_evidence_test.go) — an audit row edited by SQL is reported tampered.
- `TestAuditDeletedRowIsDetectedByCheckpoint` (same file) — a row deleted inside a sealed checkpoint is reported.
- `TestStage4936RoleGrantChangeEvidenceSessionsAndRollback` (engines/privilege_change_test.go) — a role change's before/after evidence edited by SQL no longer matches the digest in the signed audit chain.

**Not covered:** none for the scenario as written. A superuser who also holds the audit signing key (`AUDIT_SIGNING_KEY`, kept outside the database) could re-sign rows; keeping the two apart is a key-custody control, not a test.

## 4. Support impersonation

- `TestStage125OpenClientAsSupportUser` (internal/server/platform_support_test.go) — only a platform operator opens a client; the hand-off code is single-use and expires; no password signs in as the support user; a client admin cannot act on that user.
- `TestStage49106SupportSessionLeavesEvidence` (internal/server/insider_test.go) — issuing and redeeming the hand-off each write a row to the client's own audit log naming the operator.

**Not covered:** a time limit on the support session itself (it lasts as long as an ordinary session).

## 5. Malicious import or attachment

- `TestSanitizeCSVCell` (engines/import_sanitize_test.go) — imported formulas are neutralised.
- `TestStage49ExportCSVNeverCarriesAFormula` (engines/csv_safety_test.go) — exported CSV never carries a live formula.
- `TestAttachmentTypeIsSniffedFromBytes` (engines/attachments_type_rules_test.go) — renamed text, HTML, a fake JPEG and an executable are refused; a path-traversal file name is cleaned.
- `TestStage134UploadPolicy` (engines/upload_test.go) — HTML or NUL bytes in a CSV, zip path traversal, zip bombs and pictures carrying a script are refused.

**Not covered:** none for the scenario as written.

## 6. Price, refund and threshold fraud

- `TestStage472ClientSubmittedPricesNeverReachTheSale` (internal/server/pos_pricing_test.go) — a price or cost sent by the till is ignored; the master price is used.
- `TestStage472OverrideBeyondThresholdRoutesToApproval` (same file) — an override above the rule is refused and waits for approval.
- `TestStage474ReturnPricesComeFromTheOriginalSale` (internal/server/returns_test.go) — a refund is priced from the original sale, not the request.
- `TestStage474RefundCannotBeProcessedTwice` (same file) — concurrent refunds pay out once.
- `TestStage474ExceptionPathsAreSupervisorOnlyAndEvidenced` (same file) — a no-receipt return is a supervisor's, with a reason and the approver recorded.
- `TestBLD017RefundNeverExceedsPaid` (engines/bld017_economic_properties_test.go) — randomised: refunds never exceed what was paid.
- `TestStage84SelfApprovalIsCoded` (engines/approval_self_decision_test.go) — with self-approval set to nobody, a submitter cannot decide their own voucher.
- `TestStage49106bThresholdSplittingReport` (engines/insider_findings_test.go) — several records just under an approval limit by one user on one day show on the Threshold splitting report; records clear of or at the limit, on other days or by other people do not.
- `TestStage49118DetectionDrillCoversEveryRule` (engines/security_test.go) and `TestStage4954AbuseRulesIgnoreLookAlikes` (engines/security_detection_abuse_test.go) — the `threshold_split` detection rule raises a security event for two or more of one person's documents to one vendor or customer, each just under an approval limit, that reach it together within a day; look-alikes do not fire.
- `TestStage4935TwoHalvesSubmittedAtOnceBothNeedTheCombinedApprover` (engines/approval_threshold_split_test.go) — with **Approve split documents at their combined amount** on, the pieces go to the approver their total needs.
- `TestStage49106bSelfApprovalOffForNewClients` (same file) — a client provisioned from 49.10.6b on starts with self-approval off, and its admin cannot approve their own record; existing clients keep their choice.

**Not covered:** the split guard is off by default (it changes who approves what a client already routes), so until a client turns it on, the detection rule and the report flag the pieces but nothing stops them. Clients that existed before 49.10.6b keep self-approval as they had it (by default anyone, `TestStage92SubmitterMayDecideOwn`) until they turn it off.

## 7. Owner crossover

- `TestStage59MixedOwnerIsolation` (engines/wms_owner_isolation_test.go) — two owners in one warehouse: receive, allocate, pick, pack, ship, move and count never use one owner's units for the other.

**Not covered:** none for the scenario as written.

## 8. Connector SSRF and secret theft

- `TestStage134OutboundGuard` (engines/outbound_test.go) — the outbound client every connector uses refuses loopback, private, link-local and metadata addresses, a name that resolves to one, downgrade redirects and oversized answers.
- `TestBLD031ExtensionHookTargetURLRejectsPrivateResolvedHTTPS` (engines/bld031_provider_extension_contract_test.go) — a hook aimed at a private address is refused.
- `TestChannelCredentialEncryptDecryptRoundTrip` (engines/connector_test.go) — connector credentials are encrypted at rest and tamper-evident.
- `TestStage49ConnectorSecretsAreNeverReadBack` (internal/server/credentials_test.go) — no credential endpoint returns a secret, even to Super Admin.
- `TestStage49JobPayloadNeverStoresASecret` (engines/job_secret_redaction_test.go) — job payloads never store a secret.

**Not covered:** each connector's own URL handling is not tested one by one; they share the guarded client above.

## 9. Stolen backup

- `TestStage49106BackupLeavesTheBoxEncrypted` (internal/server/insider_test.go) — the backup script refuses to run without its encryption key, encrypts the database dump with AES-256, removes the plain dump, and copies only encrypted files off the box.
- `TestStage49106aMediaCopyLeavesTheBoxEncrypted` (same file) — runs the real deploy/backup.sh under bash with pg_dump, psql and curl stubbed: without the key nothing is dumped or sent, and `media_sync` alone refuses too; with it the dump and every media file (product images, expense receipts, vendor invoice copies) reach the bucket sealed with the same key and cipher, with no plain byte in them; deploy/restore_media.sh brings the files back exactly with the key and restores nothing with a wrong one.

**Not covered:** the scripts run under Git for Windows' bash in the test, not on the Linux host, and the upload is a stub - the real curl signing against the bucket is proven only by a live run (deploy/restore_drill.sh and a restore from the bucket are the operational proof). Media copied before 49.10.6a sits in the bucket as plain files until the operator deletes it - media is kept out of the bucket's lifecycle rule (49.10.6c.1, docs/operations/backup-restore.md). Someone with root on the host holds the key in /etc/erp/erp.env and reads everything regardless.

## 10. Compromised CI artifact

- `TestNoWorkflowActionIsUnpinned` (internal/supplychain/ledger_test.go) — every CI action is pinned to a full commit.
- `TestDependencyLedgerCoversGoModules` (same file) — every Go module is in the ledger with a checksum matching go.sum.
- `TestVerifyMigrationChecksums` (db/migrate_checksum_test.go) — an applied migration whose file changed is reported.

- `TestStage49106b3DeployRefusesABinaryTheReleaseMachineDidNotSign` (cmd/releasesign/main_test.go) — the real deploy/remote_deploy.sh refuses an unsigned binary, one changed after signing, and one signed by another key (even when shipped with the pinned public key) before it runs or is swapped; the first signed deploy pins its key.
- `TestStage49106b3OpenSSLAcceptsTheReleaseSignature` (same file) — the server's openssl verifies what the release machine signs, and refuses a changed binary.
- `TestStage49106b3NoReleasePrivateKeyInTheRepo` (same file) — no private key is committed beside the release or deploy scripts.

**Not covered:** the signing key lives on the release machine: whoever controls that machine, or root on the server (who can replace the binary or the pinned key directly), is outside this control. The first signed deploy trusts the key it brings (pinned on first use). `public/` and the deploy scripts are shipped beside the binary unsigned.

## 11. Destructive operator

- `TestStage91TenantDataReset` (engines/tenant_reset_test.go) — a data reset is refused while the client is active, without a fresh backup or without a reason, and is recorded.
- `TestStage4915PurgeRefusesUntilItIsSafeAndProvesRemoval` (engines/tenant_lifecycle_test.go) — a purge is refused for a live client, inside retention, under legal hold or without a backup reference.
- `TestStage4936RoleGrantChangeEvidenceSessionsAndRollback` (engines/privilege_change_test.go) — a role change is recorded with its before and after, and can be rolled back without restoring obsolete access.
- `TestStage49106bRemovalNeedsASecondPerson` (engines/client_removal_test.go) — a reset or purge runs only with a request another admin of the operator's client approved through the approval engine; the asker cannot approve it; it is spent once, for that client and act, within a week; both people are evidenced.

**Not covered:** the backup reference a purge asks for is not checked against a real backup. tenantctl names the asking admin with -actor (the approver signs in); someone with the server's shell can still reach the database directly. A sandbox's own reset and delete (Admin → Sandbox) need no second person.
