# The Project Brain

A single picture of the whole ERP — every file, grouped into brain regions, wired together by the call graph
graphify extracts from the source. It is meant to be the fastest way to answer *"where does this live and what
does it touch?"* without reading a few hundred files.

Every count in this README is deliberately approximate; the exact, current numbers are in
[BRAIN.md](BRAIN.md)'s header table, which is regenerated with the map.

| File | What it is |
|---|---|
| **[BRAIN.md](BRAIN.md)** | The brain as a document: six Mermaid diagrams, a region index, and a detail card per region. Renders in GitHub and in VS Code. **Generated.** |
| **[brain.html](brain.html)** | The brain as an interactive map: click a region, hover to light up its wiring, filter by lobe or connection strength, search by file or symbol. Open it in any browser — no server, no internet. **Generated.** |
| **[brain.map.json](brain.map.json)** | **The only file you edit.** Which regions exist, which files each one owns, and the connections no call graph can see. |
| **[update-brain.ps1](update-brain.ps1)** | Redraws the two generated files. |
| [`cmd/brainmap/`](../../cmd/brainmap/) | The generator. Go, stdlib only, no new dependency. |

---

## Redrawing it

```powershell
pwsh docs/brain/update-brain.ps1
```

That re-extracts the code graph (`graphify update .`, no API cost) and redraws both outputs. Takes a few seconds.

Two variants worth knowing:

```powershell
pwsh docs/brain/update-brain.ps1 -SkipGraph   # redraw only; skip re-extraction
pwsh docs/brain/update-brain.ps1 -Check       # also fail if any file has no region
```

> **Why a PowerShell wrapper and not just `go run ./cmd/brainmap`?**
> Windows Controlled Folder Access — Defender's ransomware protection, on by default — refuses any write under
> `%USERPROFILE%\Documents` from a binary it does not recognise, and a freshly compiled Go binary is never
> recognised. Worse, it reports that refusal as *"the system cannot find the file specified"*, which reads like a
> missing directory and is not one. The wrapper generates into `%TEMP%` with Go and copies the result in with
> PowerShell, which Windows already trusts. On a machine without Controlled Folder Access,
> `go run ./cmd/brainmap` does the identical thing and the wrapper is unnecessary.

---

## Adding to it

The brain is designed so that **growth is a one-line edit**. There are four kinds of change, in rough order of
how often you will need them.

### 1. A new file in an area that already exists

Usually **nothing to do**. Region membership is by pattern, so `engines/wms_cycle_count.go` is claimed by the WMS
region's existing `engines/wms*.go` the moment it exists. Redraw and it appears.

If the filename does not fit any existing pattern, the redraw tells you so, by name:

```
brainmap: region coverage 99.7% (1 file unclaimed)
brainmap: 1 file not claimed by any region:
  engines/leasing.go
```

That list is also written into [§6 of BRAIN.md](BRAIN.md#6-what-the-brain-does-not-know-yet), so an unfiled file
cannot quietly hide. Add a pattern to whichever region owns it:

```jsonc
{ "id": "finance", ..., "match": ["engines/finance.go", "engines/leasing.go", ...] }
```

### 2. A genuinely new area of the system

Append a region. This is the whole change:

```jsonc
{
  "id": "subscriptions",
  "lobe": "business",
  "name": "Subscriptions & Billing",
  "role": "Recurring plans, proration, dunning. One sentence on what this area is responsible for — it becomes the region's description in both outputs.",
  "match": ["engines/subscription*.go", "internal/server/handlers_subscriptions.go"]
}
```

Order in the file does not matter. When two regions could claim the same file, **the more specific pattern
wins** (more literal characters, `*` excluded), so a new `engines/pim_pricing.go` pattern beats the PIM region's
broad `engines/pim*.go` without any reshuffling. If you need to override that outright, set `"priority": 100` —
that is how the test suite claims `engines/pim_bulk_test.go` off PIM's `engines/pim*.go`.

### 3. One function that belongs somewhere other than its file

Files are the unit of ownership, but a shared file can donate individual symbols:

```jsonc
{ "id": "pos", ..., "symbols": ["handlePOSSession*", "handleCheckout"] }
```

Symbol patterns are checked before file patterns, so those functions are counted under POS wherever they live.
This exists because of real files like `internal/server/handlers_pim_pos_finance.go`, which genuinely spans five
modules — see the **Cross-module API Handlers** region, which is kept visible as its own region rather than
pretending that file belongs to one module.

### 4. A connection no call graph will ever see

An AST extractor cannot see the browser calling the server, PowerShell driving the binary, or a connector
reaching Shopify. Those are stated by hand and drawn as thick `==>` arrows so they are never confused with
measured ones:

```jsonc
{
  "from": "ui-shell",
  "to": "http-edge",
  "label": "HTTP/JSON",
  "note": "The SPA is JavaScript and the server is Go; apiFetch() calls the route table over the network."
}
```

Also hand-written, for the same reason: the `pathways` array. A call graph can tell you that A reaches B — it
cannot tell you that it happens third, or that it must. The four pathways (request, outbox, maker-checker, boot)
are ordering asserted deliberately.

**After any of these, redraw.** `brain.map.json` is validated on load: an unknown lobe, a duplicate region id, or
a declared link pointing at a region that does not exist fails loudly rather than silently vanishing from a diagram.

---

## What it can and cannot tell you

Being clear about this is the point — a diagram that overstates its own accuracy is worse than none.

**Trustworthy:**

- **Which region owns a file.** Decided by pattern, verified against the working tree, 100% covered.
- **The overall shape.** That every business area routes through the same kernel, that persistence and the error
  catalog are reached from everywhere, that the Cortex touches the rest of the system only over HTTP — these are
  aggregates over thousands of relationships and are not sensitive to any individual one being wrong.

**Needs verifying before you rely on it:**

- **Any single connection.** ~99% of cross-region relationships are graphify `INFERRED` rather than `EXTRACTED`.
  That is a property of the extractor, not a defect in the code: it resolves calls *within* a file exactly and
  calls *across* files by name — and a cross-region call is cross-file by definition. Dotted arrows are entirely
  inferred; solid ones contain at least one relationship parsed straight from source. Confirm a specific edge with
  grep before acting on it, exactly as `CLAUDE.md` requires.

**Simply not visible:**

- **Anything in a language graphify has no extractor for.** Roughly 70% of files are parsed. The rest — `.sql`
  migrations, JSON industry profiles, PowerShell, CI config, Markdown — are filed into regions by path and show up
  in the file counts, but contribute no symbols and no edges. A region can be substantial and still show few
  symbols; **Persistence & Migrations** owns 71 files and shows 15 symbols for exactly this reason. (`.sql`
  specifically needs `pip install "graphifyy[sql]"`, which is not installed here; graphify says so on every run.)
- **Runtime dispatch.** Anything reached through a registry, an event name, or a table lookup rather than a direct
  call. Where that matters it is written down as a declared link.

---

## How it is put together

```
brain.map.json  ─┐
the working tree ├─→  cmd/brainmap  ─→  BRAIN.md + brain.html
graph.json      ─┘
```

Three inputs, deliberately: **the map** says which regions exist, **the working tree** says which files exist (so
nothing can go unowned without being noticed), and **the graph** says what calls what. Only the first is written
by hand, so the picture cannot drift away from the code the way a hand-drawn architecture diagram does. Nothing
here is a new dependency — Go stdlib, no build step, no JS framework, no CDN, per the repo's first principle.

The brain files it as its own region ("The Brain Map"), so a change to the generator shows up in the diagram like
any other change.

---

## See also

- [`../README.md`](../README.md) — the documentation index: what every other doc in this repo is for.
- [`../ai_handover.md`](../ai_handover.md) — read first if you are picking up development.
- `graphify explain "<symbol>"` — the same graph, one symbol at a time, from the terminal.
- `graphify-out/graph.html` — graphify's own raw force-directed view of every node it extracted. The brain is the
  curated read of that; this is the unfiltered one.
