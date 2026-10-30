---
doc_id: DOC-INPUTVALID01
title: Canonical input validation and parser differentials
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.19.0 and later
authority: source
confidentiality: internal
last_verified: 2026-10-29
review_by: 2027-01-27
supersedes: none
superseded_by: none
---

# Canonical input validation and parser differentials

**Stage 49.4.6.** Input is checked in two places and only two: the shared
request envelope every `/api` request passes through before any handler
(`internal/server/request_envelope.go`, Stage 49.4.4), and the one validation
choke point every record save and CSV import runs through
(`validateDocumentWith`, `engines/doctype.go`). A rule added to either applies
to every route and every doctype, today's and tomorrow's.

## What a saved value may not hold

| Rule | Where | Refusal |
|---|---|---|
| A C0 control other than tab / line feed / carriage return, DEL, a C1 control, or a bidi embedding / override / isolate (U+202A-202E, U+2066-2069) in any text field | `unsafeTextRune` | 422 `GLOBAL-0309`, naming the field |
| An **identifier** that mixes a look-alike alphabet (Cyrillic, Greek, Armenian) with another alphabet - `ITEМ-01` with a Cyrillic М is a second record that reads as the first | `IsIdentifierField` + `mixedScriptRune` | 422 `GLOBAL-0311`, naming the field |
| A phone-shaped field is cleaned to E.164 before any check (Stage 41); a state is canonicalised from the GSTIN (76.2) | `NormalizeDocumentPhones`, `applyDocumentRegions` | not a refusal |

An identifier is decided from the field's name, the way the seeds name them:
`id`, `code` and `*_code`, `sku`, `barcode`, `gstin`, `pan`, `ifsc`, `hsn`,
`ean` / `upc` / `gtin`, `email` and `*_email`, `url` / `website` / `*_url`,
`account_number` and the other bank fields, `*_id`, `*_no` / `*_number`, plus
every explicit scan semantic (lot, batch, serial, LPN, wave). Phone fields are
excluded (they have their own normaliser). Names, descriptions, addresses and
remarks are free text: Hindi, Arabic, a Cyrillic brand inside an English
sentence all pass. An identifier written wholly in one alphabet passes too;
only the mix of a look-alike alphabet with another is refused.

## The generic list's sort and filter vocabulary

`GET /api/v1/doc/{doctype}` takes an allowlist, never an expression:

| Parameter | Accepted | Otherwise |
|---|---|---|
| `sort` | absent (id order) or `recent` (last change first) | 422 |
| any other name | one equality on a field the doctype declares (or its `id` / `status`); the name must match `^[a-zA-Z_][a-zA-Z0-9_]{0,63}$` | 422 (an operator suffix such as `code__gt`, a column the doctype does not have, a name that is not a name) |
| `q`, `q_fields`, `limit`, `offset`, `count` | as documented for the list | a non-numeric `limit` / `offset` falls back to the default |
| a repeated parameter (`?code=a&code=b`) | - | 400 `GLOBAL-0307` (49.4.4a: ambiguous) |

Values are always bound as parameters; a field name reaches SQL only after
the allowlist above. `TestStage4946ListSortAndFilterAllowlist` pins each row.

## Parser differentials

Each shape below could be read differently by a proxy, Go's parser,
PostgreSQL and a browser. Each has one answer, the answer is never a 5xx, and
every refusal is the JSON error envelope. `TestStage4946ParserDifferentialsAnswerConsistently`
sends each one over a raw TCP connection to the real server stack and holds
it to this table.

| Shape | Answer | Why |
|---|---|---|
| A plain, well-formed JSON save | 200 | the reference |
| The same key twice in one JSON object | 400 `GLOBAL-0307` | Go's decoder would silently keep the last value; the envelope refuses the object (49.4.4) |
| An amount sent as a JSON string (`"150.5"`) and as a number (`150.5`) | both 200, both stored as the same number | the validator reads both as one value |
| An amount with a unit (`"12abc"`) | 422 `GLOBAL-0002` | the whole value must be a number |
| A number with a leading `+` or leading zeros (`+5`, `05`) | 400 `GLOBAL-0307` | not JSON numbers; refused rather than read as 5 |
| `1e308` | 400 `GLOBAL-0307` | above 2^53-1 a float64 silently rounds; the envelope bounds every JSON number |
| `1e400` (overflows float64) | 400 `GLOBAL-0307` | same bound; never an infinity |
| `-1` for an amount | 200 (the doctype's own rules decide the sign) | a negative amount is a business rule, not a parser rule |
| `Transfer-Encoding: chunked` together with a `Content-Length` that disagrees | the chunked body is read, the length is ignored: 200, the plain save's answer | Go's server drops `Content-Length` when the body is chunked (RFC 9112 §6.3); no second reading exists for the proxy to disagree with |
| A `Content-Length` longer than the body sent | 400 `GLOBAL-0307` | "the request body could not be read completely" |
| A query value URL-encoded (`%E0%A4%95`) and as raw UTF-8 (`क`) | the same 200 and the same rows | one decoding, before the handler |
| `%2F` inside a path segment (`/doc/Customer/A%2FB`) | 404 `GLOBAL-0004` | one segment, an id holding a slash; it never splits the route |
| `..%2F..%2Fetc` as a path segment | 404 `GLOBAL-0004` | an id, not a walk |
| A raw `../` segment in the path | 307 to the cleaned path | Go's mux canonicalises before routing; the cleaned path is then routed as any other |

## What is not here

The browser is not a parser this server trusts: every rule above runs on the
server, and the browser's own filters (`public/field-formats.js`, the phone
keystroke filter) exist for the typist's benefit only.
