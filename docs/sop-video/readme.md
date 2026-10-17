---
doc_id: DOC-SOP-VIDEO-README-20261005
title: Modular ERP process video library
type: procedure
status: draft
owner: product-owner
approvers: [operations-owner, finance-owner]
audience: [trainers, operators, administrators]
applies_to: 2026-10-05 local training edition
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-11
review_by: 2026-11-05
supersedes: none
superseded_by: none
---

# Modular ERP process videos

Open [the chapter player](index.html) for step navigation, or play a whole
chapter as one seekable video from [output/](output/) - one file per chapter,
named by its id (for example [output/online_order.webm](output/online_order.webm)).
As of 2026-10-11 there are **25 chapters, one per module, all 64 steps recorded**;
[coverage.md](coverage.md) lists what each module has and what is not yet filmed. This is still a **draft training edition**: the clips come from a
disposable training database, there is no voice track, and no business owner has
signed a chapter off. [output/all-ready.webm](output/all-ready.webm) is the
earlier Purchase Requisition walkthrough, kept as it was.

The recordings themselves (`media/` and `output/`, about 500 MB) are **not in git** -
they live on the machine that recorded them and are rebuilt from the tracked
scripts and tools here. How the finished chapters reach clients is still to be
decided (checklist 57.12a); once it is, this paragraph will say where they are.

## How the chapters work

Each chapter in [project.json](project.json) has an explicit start, ordered steps,
actor and end. Prerequisite setup is *referenced again* in every dependent process:
the Vendor setup clip appears in both PO and GRN, for example. A viewer can start
at any chapter without relying on an earlier video. See the exact human narration
and captions in [scripts.md](scripts.md), and the release/gap register in
[coverage.md](coverage.md).

To replace a changed step, update its script and `tools/scenes.cjs`, record only
that segment, verify its UI save and readback, then copy its reviewed WebM to
`media/<segment-id-in-kebab-case>.webm`. Keep the segment ID and referenced chapters
stable. If the changed master data affects later transactions, mark those dependent
clips stale and re-record them too. Never publish a chapter with pending or stale
segments. The previous approved edition should remain available during review.

## Local playback and reassembly

Run `node docs/sop-video/tools/serve.cjs` at the project root, then open
`http://127.0.0.1:8122/`. The loopback player serves video byte ranges for seeking.
Run `node docs/sop-video/tools/validate.cjs` and
`node --test docs/sop-video/tools/video.test.cjs` after any manifest or player
change. To reassemble complete chapters, set `SOP_OUTPUT_DIR` to a private scratch
directory outside this repository and run
`node docs/sop-video/tools/assemble.cjs all-ready`; inspect the output before
copying it into `output/`. The assembler includes start, step and end cards and
uses Playwright Chromium and its FFmpeg binary. Incomplete chapters are rejected.

`tools/record.cjs` uses Playwright against a **loopback-only** test ERP and requires
`SOP_STORAGE_STATE` pointing to a private authenticated storage-state file; never
commit that file, access tokens or live customer data. Use an isolated disposable
database, test personas and clearly labelled fixtures. The recording command is
`node docs/sop-video/tools/record.cjs <segment-id>`, with `SOP_BASE_URL`,
`SOP_OUTPUT_DIR` and `SOP_FIXTURE_DIR` set for the local session. Review the raw
clip and transaction readback before copy-in. Human narration can be added later
per segment; replacing one segment must never require re-recording unrelated steps.
