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
last_verified: 2026-11-15
review_by: 2027-02-13
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

## What fails a capture, and what each shot is tagged with

Every selected screen is checked before its image is kept. The whole set is refused (nothing is
published) on any of: an HTTP status of 400 or more; a browser console error or uncaught page
error; a failed network request; an unexpected dialog or overlay; a missing in-app help
mapping (the screen has no entry in `GET /api/v1/help/index` `screen_map`, so the Help drawer
would be empty; a shot may set `help: false` only for a screen that has none by design);
a declared selector (`selectors`) that is no longer on the page (a stale selector, named in the
message); navigation that fell back to another screen; a session whose role or tenant does not
match; clipped controls; and a crop larger than the viewport.

Role boundaries are part of the evidence. A shot may list `refusedRoles`; when the capture runs
as one of those roles the screen **must** be refused. A refusal (a 401/403, a redirect away, or an
access-denied screen) is recorded as `refused-as-expected` with no image; the screen opening is the
failure (`unauthorized success`). A shot may list `allowedHttp` statuses for an expected denial.
A refusal screen is recognised by its wording (access denied, do not have permission, no access, ask your administrator). A closed modal overlay (opacity 0 on an ancestor) is not an unexpected dialog. Both were found by running the tool as Cashier against the real app (2026-11-10): roles and configuration were refused as expected; reports and pos-billing were captured, so the help-map check passed on real data.

Each image has a sidecar `<id>.json`, and the manifest (`schema_version` 2) carries the same
tags: `release`, `commit`, `role`, `fixture`, `tenant_fixture`, `viewport`, `device_scale`,
`locale`, `theme`, `outcome`, `route` and the image `sha256`. Pass `--commit <sha>` for the exact
build under capture.

Each failure mode is proved by injection in `capture-screenshots.test.cjs` (a synthetic page
that breaks that one rule must make the capture fail, and an HTTP error, console error, page error,
overlay, missing help mapping, unauthorized success, stale selector and clipped control each have
their own fixture screen).

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

`docs/guides/img` holds only the current manual set: the images a guide, SOP or KB
article shows, each with a shot in the capture tool. `TestManualImagesAreTheLivingSet`
(`go test ./internal/kb/`) fails on an image no manual shows, a missing image, a file
that is not a manual image, and an image with no shot. When a manual stops showing
an image, move it to `docs/archive/legacy/screenshots-<date>/` and add a row to the
archive's "Where did it go?" table. The 2026-08-01 set, its manifest and its three
unused shots are in [the archive](../archive/legacy/screenshots-2026-08-01/screenshot-manifest.md).

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
