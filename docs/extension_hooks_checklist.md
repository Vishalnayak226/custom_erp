# Client Extension Layer — Safety Checklist

Tracks the "hired 3rd-party developer builds a custom layer for one client, isolated from other
tenants, unaffected by our own core patches" mechanism. The mechanism itself already exists
(Stage 14.17-14.20, `docs/micro_checklist.md`, done 2026-07-18) — this file tracks operational
gaps and process reminders around it, not the base build. Append new items as they come up.

> See also: [`github_checklist.md`](github_checklist.md) (repo/org access controls) and
> [`Contract/Developer Contract.md`](Contract/Developer%20Contract.md) (legal side for *our own*
> hired developers). This file is specifically about **a client's own hired developer** building
> against our API/webhook surface — they should never need repo or DB access at all; if a request
> ever comes in for either, treat it as a red flag and push back toward the extension-sdk contract
> instead.

## What already exists (verified against code, 2026-07-22)

- [x] Tenant + doctype scoped webhook hooks (`engines/extensions.go`): `document.before_save`
      (synchronous, can block a bad save) / `document.after_save` (async, fire-and-forget).
- [x] HMAC-SHA256 signed payloads (`X-Signature` header), timeout-bounded + panic-isolated calls —
      a broken 3rd-party endpoint can't hang or crash the host process, only stall that one
      tenant's saves on that one doctype.
- [x] `extension-sdk/README.md` + `hook-payload.schema.json` — the entire handoff packet for a
      hired 3rd-party developer. Zero dependency on the rest of the repo; meant to be copied into
      its own git repo before a real handoff. Contains an explicit "what you will never receive"
      section (no core source, no other tenant's data/secrets, no full session token).
  - [ ] **Reminder:** confirm this actually gets copied to a *separate* repo at the next real
        handoff — don't hand a 3rd-party developer access to this repo, "just to grab the SDK folder."
- [x] Scoped, read-only extension tokens (`SignExtensionToken`) — locked to one tenant + one
      doctype, no role, can't log into the UI, can't hit any other endpoint.
- [x] Schema-per-tenant isolation — hooks live in each tenant's own schema, invisible to other tenants.
- [x] Verified live (2026-07-18): allow/reject/timeout/after-save-failure behavior all confirmed
      correct against a simulated 3rd-party server; a hung hook confirmed *not* to block an
      unrelated concurrent request.

## Gaps to close (found 2026-07-22)

- [x] **No admin UI — closed 2026-07-23.** New "Extension Hooks" screen under Settings
      (`public/app.js`'s `renderExtensionHooksView`/`renderExtensionHookLogView`, nav entry in
      `public/index.html`), reusing the same `.table-panel`/inline-form conventions Accounting
      Periods (Stage 20.34) established: a register-hook form + list table (View Log/Delete
      actions) and an issue-token form, both against the existing API endpoints - zero backend
      change needed. A new `showOneTimeSecretDialog` (4th use of the existing custom-dialog chrome
      alongside showCustomAlert/Confirm/Prompt) shows a registered hook's secret or an issued
      token's value once, as a readonly pre-selected input. **Live-verified** end-to-end via
      Playwright against a throwaway instance (port 8145, real `custom_erp` DB, a scratch-minted
      HR/Admin token, no real user/session touched): registering a hook with a non-https target
      URL was correctly rejected inline with the backend's own error text, a valid https hook
      registered and showed its 64-char secret in the one-time dialog, the row appeared in the
      list, View Log opened the log screen (empty - the target URL was a fake `example.com`
      endpoint that was never actually called), Issue Token showed a 225-char token in its own
      one-time dialog, and Delete removed the row (confirmed 0 rows after). No console errors
      beyond the expected 404 favicon and the intentionally-rejected registration's 422. Scratch
      server stopped, scratch DB rows removed via the same UI-driven delete, scratch token-minting
      tool deleted afterward.
- [x] **No HTTPS enforcement on `target_url` — closed 2026-07-23.** New `validateHookTargetURL`
      (`engines/extensions.go`) rejects any `target_url` whose scheme isn't `https://`, called from
      `RegisterExtensionHook` before anything is persisted. Plain `http://` stays allowed only for
      `localhost`/`127.0.0.1`/`::1`/`*.localhost` so local development against the extension-sdk
      still works without a real TLS cert. Covered by `TestExtensionHookTargetURLValidation`
      (`engines/extensions_test.go`) and live-verified via the new admin UI above.
- [x] **No automated test coverage for `engines/extensions.go` — closed 2026-07-23.** New
      `engines/extensions_test.go`: `TestInvokeBeforeSaveHooksSignatureAndPayloadShape` (payload key
      shape + a real HMAC-SHA256 signature check computed independently from the hook's secret, the
      exact contract `extension-sdk/README.md` documents to a 3rd-party developer), plus tests for
      the non-2xx-blocks-the-save path (`EXT-0289`), the timeout-blocks-the-save path (`EXT-0290`),
      the no-matching-hooks fast path, and that `InvokeAfterSaveHooksAsync` never blocks its caller
      even against a slow endpoint. All 6 new tests pass (`go test ./engines/... -run
      'TestExtensionHookTargetURLValidation|TestInvokeBeforeSaveHooks|TestInvokeAfterSaveHooksAsync'
      -v -p 1`); full suite re-run clean except the two pre-existing, already-documented failures
      (`TestEngines/FinanceDoubleEntryAndPOS`, `TestEngines/DocTypeValidationAndAuth`) - confirmed
      via `git stash` to fail identically on the unmodified baseline, unrelated to this work.
- [ ] **No early revocation for scoped extension tokens.** `SignExtensionToken` issues a stateless
      HMAC token with no server-side revocation list — a leaked token can only be neutralized by
      waiting out its TTL (or rotating `JWT_SECRET`, which invalidates every token platform-wide).
      Mitigate by defaulting to short TTLs (the SDK example uses 60 min); consider a revocation
      list only if a real incident makes it necessary — don't build it speculatively.
- [ ] **Only two hook points exist** (`document.before_save`/`document.after_save`). If a client's
      custom layer needs `on_submit`, `on_cancel`, or a scheduled/cron trigger, that's not
      supported yet — extend `RegisterExtensionHook`'s allowed `hook_point` values and the relevant
      call site only when an actual client need shows up, not ahead of time.
- [ ] **Doctype-rename hazard.** Renaming a doctype in a future patch silently breaks any hook
      registered against the old name (no error, it just stops matching). Add this to the release
      checklist: before renaming a doctype, check `extension_hooks` across tenant schemas for
      existing registrations against it.

## Process reminders for every future core patch

- [ ] Treat the hook payload shape (`hook_point`, `doctype`, `document_id`, `tenant_id`, `data`)
      as an **append-only public contract** — new optional keys are fine, renaming/removing an
      existing key is a breaking change for every client extension relying on it.
- [ ] Treat `extension-sdk/` itself the same way you'd treat a public API version — if it ever
      needs a breaking change, that's a new SDK version with a migration note, not a silent edit.
- [ ] Before shipping a patch that touches `handleGenericDoc`'s save path, `engines/extensions.go`,
      or `engines/auth.go`'s token claims, re-run `engines/extensions_test.go` (added 2026-07-23,
      see the Gaps-to-close entry above).

## Legal / ownership note

A client's own hired developer builds *their* custom service against this API/webhook contract —
that service and its code is entirely theirs; you never take on ownership or liability for it.
Make sure your SaaS Terms of Service (or a short customization addendum) states plainly: (a) you're
not responsible for uptime/bugs in a client's own custom extension, (b) their custom code is never
shared with or visible to other tenants, (c) they get API/webhook credentials only — never source
code or database access, and if a request for either comes in, it should be redirected back to this
extension surface instead of granted.

---

## Log

- 2026-07-22: Checklist created. Base mechanism (Stage 14.17-14.20) confirmed already built,
  shipped, and verified live on 2026-07-18 — this file starts from real gaps found today, not a
  ground-up design.
- 2026-07-23: Closed the 3 buildable gaps found 2026-07-22 (admin UI, HTTPS enforcement, automated
  test coverage) — see each item above for detail. The remaining 2 items in this section (token
  revocation, additional hook points) are deliberately still open per their own text: both say not
  to build them speculatively/ahead of an actual need. The doctype-rename hazard reminder and the
  "process reminders for every future core patch" section are standing policy, not one-time build
  items, and stay open by design (nothing to check off). `docs/github_checklist.md` was reviewed in
  the same pass but every item there is a live GitHub org/repo setting (2FA, branch protection,
  billing role, etc.) — operator action on shared infrastructure, not code, so intentionally left
  for the user rather than changed unilaterally.
