# Standardized Message Catalog (Stage 23)

> **Status (2026-07-20, complete; follow-up closed 2026-07-21)**: catalog generated (302 codes) and live; all framework-level paths (auth, rate limiting, panic recovery, module/feature gating) and all error-response call sites across all 11 `internal/server/handlers_*.go` files (including `handlers_finance_maturity.go`, swept 2026-07-21) converted to the standardized envelope. Frontend Toast/Page Banner primitives added; `showApiError()` now dispatches to them automatically based on each response's catalog-derived `display_style`. The two open product decisions from the first pass (400-vs-422 status convention, `SAAS-0192` soft-200) were both resolved 2026-07-21 — see "Known conflicts / gaps" below. `go build`/`go vet`/`go test ./... -p 1` clean; manually verified against a live throwaway instance (401/USERAC-0021 bad login, 401/GLOBAL-0009 no-token, 429/SEC-0280 rate limit — all returned the correct standardized JSON body). This doc is the canonical reference; `docs/specs/implementation_plan.md` §6 and `docs/requirements/PRD.md` §5 point here instead of duplicating detail.

## What this is

Every user-facing error/warning/info/success message in the ERP is now defined once, in one place, instead of being a hand-typed string at each call site. The source of truth is a spreadsheet the product owner maintains:

```
C:\Users\ABCD\Downloads\MyBusiness\IT Solution\ERP\Doc\ERP_Standard_Message_Control_Matrix_Final.xlsx
```

(a dated backup of the pre-Stage-23 version lives alongside it as `ERP_Standard_Message_Control_Matrix_Final.backup-2026-07-20.xlsx`). Its `Final Matrix` sheet has one row per message: a Message ID (`MSG-0001`...), an Error Code (`GLOBAL-0001`, `MASTER-0040`, ...), the module, the scenario that triggers it, the standard user-facing wording, severity, HTTP status, display style, and whether it must be logged/audited.

## How it gets into the codebase

`scripts/gen_error_catalog.py` (Python + openpyxl, **dev-time only** — never imported or run by the Go binary, so it adds no runtime dependency) reads the xlsx and regenerates `internal/server/error_catalog_generated.go`: a `map[string]CatalogEntry` keyed by Error Code. Regenerate after any change to the xlsx:

```
py scripts/gen_error_catalog.py
```

`CatalogEntry` fields: `Code, MessageID, Module, Scenario, MessageType, UserMessage, UserAction, Severity, Blocking, DisplayStyle, HTTPStatus, Retryable, LogRequired, AuditRequired, Priority, RequirementLevel`.

## The response envelope

`internal/server/apierror.go` provides two functions every handler uses instead of `http.Error`/hand-rolled `json.Encode`:

- **`writeAPIError(w, r, code, subFor)`** — precise: looks up a catalog code, writes its standard message/status, substitutes the message's single `{placeholder}` with `subFor` if given.
- **`writeAPIErrorGeneric(w, r, status, message)`** — fallback: keeps the call site's own message text, wraps it in the same envelope, auto-picks the nearest Global/Common code by HTTP status.

Every error response now has the same shape:
```json
{"error": "Item code already exists. Please use a different code.", "code": "MASTER-0040", "correlation_id": "…", "retryable": false, "display_style": "Modal popup"}
```

`display_style` (added 2026-07-21) passes the catalog row's own display style straight to the client so `showApiError()` (`public/app.js`) can dispatch to the matching non-blocking primitive — see "Display styles → frontend primitives" below.

This fixed a real, live bug: `apiMiddleware` sets `Content-Type: application/json` on every response, but the previous plain-text `http.Error(...)` calls broke every frontend `await res.json()` caller on that path — the user saw a generic "Unable to reach the server" instead of the actual message. `apiFetch`/`apiUpload` (`public/app.js`) now read the real `error`/`code` from the body instead of a hardcoded string for 401/429s.

Logging: per code, `LogRequired`/`AuditRequired` drive calls into the *existing* `engines.LogSystemError`/`engines.LogAuditEvent` (no DB schema change — the code is prefixed onto the logged message, e.g. `[MASTER-0040] ...`, so it's grep-able in the existing log tables without a migration).

## Display styles → frontend primitives

| Display Style (row count) | Frontend primitive | Status |
|---|---|---|
| Page banner (152) | `renderPageBanner()`, auto-dispatched from `showApiError()` via `display_style` | Wired (Stage 23 follow-up, 2026-07-21) |
| Inline field message (92) | existing `.login-error`-style per-field `<div>` pattern | Reused, unchanged |
| Modal popup (28) / Confirmation dialog (1) | existing `showCustomAlert`/`showCustomConfirm` — also `showApiError`'s fallback for every display style with no generic dispatch target | Reused, unchanged |
| Toast (23) | `showToast()`, auto-dispatched from `showApiError()` via `display_style` | Wired (Stage 23 follow-up, 2026-07-21) |
| Inline message (5) | existing inline-field pattern (same primitive, different copy) | Reused |
| Inline grid message (1) | — | Backlog — no paste-into-grid feature exists to attach it to |

`showApiError(res, fallback, title)` (`public/app.js`) reads the response body's `display_style` and dispatches: `"Toast"` → `showToast()`, `"Page banner"` → `renderPageBanner()` against the app's `#view-root` container, everything else falls back to the existing blocking `showCustomAlert(message, title)` — Inline field/grid message styles need per-field context no generic response-handling helper has, so they deliberately keep the modal fallback rather than rendering incorrectly. This covers every one of `showApiError`'s ~50 callers (32 original + ~19 converted from hand-rolled duplicate logic in the same follow-up) automatically, for any current or future precise catalog code — not a per-screen migration. A full screen-by-screen audit of the remaining raw `showCustomAlert` call sites that *don't* go through `showApiError` (mostly client-side-only validation with no response object, or non-error success confirmations) is still not done — tracked as Stage 23 backlog in `micro_checklist.md`.

## What's wired vs. defined-only

Not every one of the 302 catalog codes is called from real code, and that's intentional:

- **Wired**: framework-level paths (session/token, rate limit, module/feature gate, panic) plus every handler's existing error call sites, upgraded to a precise code wherever the scenario clearly matches one **and** the catalog's HTTP status matches what the endpoint already returned (never a silent status-code change), generic-coded otherwise.
- **Defined but not wired**: catalog codes for the ~187 "Mature ERP"-tier matrix rows that describe modules/features not yet built (deep SaaS billing, PIM connectors beyond the 3 built, Omnichannel ATS specifics, deployment automation, backup/DR, POS cash drawer, extension-hook governance, etc.). Building new business validation isn't in scope for a messaging-standardization pass — these are tracked by MSG-ID in `micro_checklist.md` Stage 23's backlog list so they aren't silently dropped, and get wired when the underlying feature is actually built.

## Handler sweep results (per file)

| File | Call sites converted | Precise catalog code | Generic fallback |
|---|---:|---:|---:|
| `handlers_operations.go` | 87 | 0 | 87 |
| `handlers_pim_pos_finance.go` | 81 | 4 | 77 |
| `handlers_core_doc_engine.go` | 70 | 8 | 62 |
| `handlers_integrations_admin.go` | 66 | 0 | 66 |
| `handlers_procurement_pim2.go` | 33 | 0 | 33 |
| `handlers_admin_identity.go` | 20 | 0 | 20 |
| `handlers_auth.go` | 19 | 6 | 13 |
| `handlers_profile.go` | 12 | 0 | 12 |
| `handlers_wms.go` | 12 | 0 | 12 |
| `handlers_finance_maturity.go` | 13 | 0 | 13 |
| `middleware.go` (framework paths) | ~6 | ~5 | ~1 |
| **Total** | **~419** | **~23** | **~396** |

`handlers_finance_maturity.go` was deliberately excluded from the original sweep — it was actively being written by a concurrent session as uncommitted work at the time. Swept 2026-07-21 once that work landed (commit `99d63de`), generic-fallback only (see `micro_checklist.md` 23.9 for why no precise codes were attached).

**400→422 status conversion (2026-07-21)**: separately from the code-attachment sweep above, every `http.StatusBadRequest` in these files (186 occurrences across the original 10, plus `handlers_finance_maturity.go`'s own 13) was converted to `http.StatusUnprocessableEntity`, resolving the 400-vs-422 gap noted below. This was a pure status-code change, not a code-attachment change — the precise/generic counts in the table above are unaffected by it.

## Known conflicts / gaps found during this pass

- **The catalog has zero rows at HTTP 400, and zero at 405.** Its status distribution is 175×422, 43×200, 42×409, 24×503, 8×403, 6×401, 2×404, 1×429, plus the one gap-fill row at 500. The matrix's own convention is `422 Unprocessable Entity` for validation-style errors; a large fraction of the existing codebase instead used `400 Bad Request` for the same kind of error, and every handler uses bare `405` for its method-not-allowed guards (405 stays generic-coded permanently — it's a routing-level concern the matrix was never going to have a row for, unrelated to the 400-vs-422 question). **Decided 2026-07-21 (`micro_checklist.md` 23.12): the codebase standardizes on 422.** Every `http.StatusBadRequest` across all 11 `handlers_*.go` files was converted to `http.StatusUnprocessableEntity` (see the handler-sweep table above) — this unblocks precise-code attachment for scenarios that were previously skipped purely on the status mismatch (e.g. `POSOFF-0237`/`POSOFF-0238`, `GLOBAL-0001`/`GLOBAL-0014`), though re-attaching those specific precise codes is a separate, not-yet-done follow-up (the 400→422 sweep itself was purely mechanical, not a re-audit of all ~400 call sites against the catalog). `FIN-0260` ("period locked") is a different, unrelated status conflict — the matrix pins it to `409`, not `422`, so this decision doesn't resolve it.
- **`INT-0220` ("Webhook signature invalid") status conflict**: matrix says `422`; the codebase's Shopify/BigCommerce webhook signature checks return `401`. Same "don't silently change status" reasoning — left generic-coded rather than force either side to change.
- **`EXT-0291`/`EXT-0289`/`EXT-0290` (extension hook token-scope / timeout) status conflicts**: matrix pins these to 401/422/503; the actual extension-hook code paths use 403 (doctype-scope mismatch) and 502 (hook didn't respond in time) — neither status exists in the catalog at all for those scenarios.
- **`SAAS-0192` ("Feature flag disabled") status conflict — decided 2026-07-21 (`micro_checklist.md` 23.13): keep `featureGate`'s fail-closed `403`.** The matrix specifies a soft `200`/Inline-message response, but a disabled-feature request is exactly the kind of authorization check that should never look like success to a caller that didn't pass it — this codebase's own frontend fetch helpers treat any 2xx as "succeeded," so a soft 200 would force every caller to separately inspect the body to discover the action never ran. Kept the existing `403` behavior (generic-coded); the matrix's `200` was considered and deliberately not adopted, not left unresolved.
- **`GLOBAL-0302` ("Unexpected server error", 500)** — added to the xlsx and catalog during this pass. The original 301-row matrix had no row for the panic-recovery path (no row anywhere used HTTP 500), so the global panic handler had nothing to map to.
- A handful of scenarios have no catalog row at all regardless of status (flagged by the sweep, not yet added to the xlsx — small enough in number and value to leave as a documented list rather than force into the source spreadsheet): "Approved transactional documents cannot be deleted", "Only master documents can be reactivated" (`handlers_core_doc_engine.go`), and the generic method-not-allowed guard text across every handler file.
- Legacy `docs/specs/implementation_plan.md` §6 (11 semantic codes like `ITEM_DUPLICATE`) was cross-checked against the new catalog: all 11 scenarios have a clean match (see that file's superseded-notice). Nothing was lost in the transition.
