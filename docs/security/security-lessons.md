---
doc_id: DOC-SECLESSONS01
title: Security lessons and their regression tests
type: record
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.16.0 and later
authority: source
confidentiality: internal
last_verified: 2026-10-26
review_by: 2027-01-24
supersedes: none
superseded_by: none
---

# Security lessons and their regression tests

**Stage 49.17.8.** Every security incident, near miss, assessor finding,
support workaround or performance regression that taught something is one row
here, and every row names the test that now fails if the mistake comes back.
`TestStage49178EveryLessonHasALiveRegressionTest` (cmd/gendocs) fails when a
named test no longer exists, so a lesson cannot quietly lose its guard. Add the
row in the same change as the fix.

| Date | Source | What was wrong | Fix | Regression test |
|---|---|---|---|---|
| 2026-09-23 | assessor finding | Two concurrent deliveries of the same channel webhook could each create a sales order | advisory lock around channel order import | `TestBLD031ConcurrentDuplicateChannelWebhookDeliveryIsIdempotent` |
| 2026-10-25 | assessor finding | A password alone earned an MFA challenge token that worked as a full session | purpose tokens reach only their own MFA step | `TestStage49PasswordAloneNeverOpensAnMFAAccount` |
| 2026-10-25 | assessor finding | One reset link used twice at once set two passwords | the update claims the token itself | `TestStage49PasswordResetTokenWinsOnce` |
| 2026-10-25 | assessor finding | A purged client's extension token resolved to the default client | extension tokens pass the client gate | `TestStage49TokenForAnUnregisteredClientIsRefused` |
| 2026-10-25 | assessor finding | Another store's record answered 403, a missing one 404 - existence disclosed | both answer 404; the refusal is still audited | `TestStage49OutOfScopeAndMissingRecordsAnswerAlike` |
| 2026-10-25 | assessor finding | Five CSV exports could carry a spreadsheet formula | one safe CSV writer | `TestStage49ExportCSVNeverCarriesAFormula` |
| 2026-10-26 | near miss | An abandoned report run kept its goroutine and database connection uncounted | live runs bounded per client and server | `TestStage49133AbandonedReportRunsAreBounded` |
| 2026-10-26 | support workaround | The nightly reconciliation ran GST Return Summary with no dates and failed every night | required dates filled | `TestStage49135ReconciliationFillsRequiredDates` |
| 2026-10-26 | assessor finding | A suspended or trial-expired client's campaigns, dunning, e-invoices, webhooks and jobs kept running | workers serve only operational clients | `TestBLD028WorkersServeOnlyOperationalTenants` |
| 2026-10-26 | assessor finding | Retention sweeps deleted logs from a client under legal hold; four logs had no sweep at all | one batched, hold-aware delete; every state table registered | `TestStage49187RetentionDeletesInBatchesAndHonoursLegalHold` |
