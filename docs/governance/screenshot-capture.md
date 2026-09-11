---
doc_id: DOC-GOV-004
title: Screenshot capture and review
type: procedure
status: draft
owner: documentation-maintainer
approvers: [qa-owner, product-owner]
audience: [documentation-author, qa-owner]
applies_to: current manual screenshots; synthetic fixtures only
authority: canonical
confidentiality: internal
last_verified: 2026-09-08
review_by: 2026-10-08
supersedes: none
superseded_by: none
---

# Screenshot capture and review

Capture against an isolated fixture with invented business data, using the
actual role whose task the manual describes. Select only the screens that role
is expected to access. A successful administrator screenshot cannot establish
cashier or floor-user access.

## Prepare a review set

1. Record the fixture version and the exact release/commit deployed to it. Confirm
   the selected screens contain no real customer, employee, payment or secret data.
2. Sign in through the normal login/MFA flow in a local Playwright browser. Save
   its storage state outside the repository using `context.storageState({path: ...})`.
   Treat that file as a credential; never commit it or copy it into the output set.
3. Run the [capture tool](../guides/capture-screenshots.js). Playwright must already
   be available as local documentation tooling; it is not an application dependency.

```powershell
node docs/guides/capture-screenshots.js `
  --base http://localhost:8152 `
  --storage-state "$env:TEMP/erp-doc-session.json" `
  --out "$env:TEMP/erp-shots-review-001" `
  --release "0.1.0-<deployed-commit>" `
  --role "Cashier" --tenant "docs-fixture" --fixture "retail-fixture-v1" `
  --only pos-billing --viewport 1440x900 --locale en-IN --theme light
```

The output directory must not exist. A new complete set is published only when
every selected shot passes. The script never merges a partial run into existing
manual assets. It follows the application's real deep links and verifies the
session's role against the profile endpoint; it does not manufacture a username
or elevated role. Release and fixture labels are supplied by the operator and
must be checked against the deployed artifact during review.

The JSON manifest records release, role, fixture, viewport, scale, locale, theme,
time, route and image hashes. It excludes the server address, username, storage
state and token. The tool fails for HTTP/browser errors, an authentication or
denial screen, unexpected overlays, navigation fallback and detected clipping.
Controls inside an intentional scroll area are allowed. Cropped sidebar shots
require a viewport large enough for their declared crop.

## Review before replacing living assets

Inspect every image at its intended display size and with keyboard/screen-reader
users in mind: task state, wording, contrast, meaningful caption/alt text, visible
controls, privacy, overlays and any clipping the DOM checks missed. Record the
reviewer, source release, fixture, findings and accepted image hashes. Automated
pass status remains `pending-human-review` until that record exists.

Publish only the reviewed, explicitly listed current manual files. Preserve old
assets and their references until parity, replacement and retention checks pass;
capture does not authorize their deletion. Existing `guides/img` material has not
been recertified by the new harness. Real RF device and screen-reader walkthroughs
remain separate evidence.

## Verify the harness

Full-page captures are paced 30 seconds apart by default so repeated application startup
requests stay within the real server's rate limits. `--interval-ms` accepts 0–60000 for an
explicit fixture cadence; the manifest records it. Rate-limit or other HTTP failures still
abort the set. Capture never disables server controls or retries through a refusal.

```powershell
node --test docs/guides/capture-screenshots.test.cjs
```

This exercises a synthetic HTTP fixture in a real Chromium browser, including
error, denied-access, wrong-role, unexpected-overlay, clipped-control, navigation
fallback, preservation of an existing set and failure after an earlier successful
shot. It is harness evidence, not a live ERP release acceptance run.
