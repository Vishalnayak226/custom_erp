---
title: Reading and writing records through the API
section: Reference
order: 41
summary: Your first call, then list, read, create, change, submit and sync any document type your key's modules cover.
audience: integrator
screens: [api-access]
public: true
last_verified: 2026-11-16
doc_id: DOC-AE4FF88C8C
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-14
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: reference
module: platform
task: Reading and writing records through the API
prerequisites: A module key from Settings → API access (see Authenticating against the public API)
---

# Reading and writing records through the API

A module key reaches every document type in the modules it was given:
**View** reads them and **Change** also creates, edits and submits them. Each
call runs as the key's owner, so the same rules apply as on the screens: field
rules, store limits, approval steps and status checks.

## Your first call

```
curl https://<your-server>/api/public/v1/me   -H "Authorization: Bearer erp_v2_..."
```

The answer names the key, its owner, its modules and document types, whether it
can approve, its stores, its expiry and its limits. If this works, the key works.

## The routes

| Call | Does |
|---|---|
| `GET /records/{doctype}` | List records (newest first) |
| `GET /records/{doctype}/schema` | The fields, with type, whether required, options and length; custom fields included |
| `GET /records/{doctype}/{id}` | One record |
| `POST /records/{doctype}` | Create a draft (**Change** needed) |
| `PUT /records/{doctype}/{id}` | Change a draft |
| `DELETE /records/{doctype}/{id}` | Delete a draft (only drafts, as on the screens) |
| `POST /records/{doctype}/{id}/submit` | Submit it |
| `POST /records/{doctype}/{id}/cancel` | Cancel it |
| `POST /records/{doctype}/{id}/approve`, `/reject` | Only for a key with **Can approve** |

All paths start with `/api/public/v1`. `{doctype}` is the document type's
name, for example `PurchaseOrder` or `Item`. Some ids contain `/`
(`PO/B41/2627/00008`): encode the id in the path (`PO%2FB41%2F2627%2F00008`),
or the call answers 404. Every `POST`, `PUT` and
`DELETE` needs an `Idempotency-Key` header - see
[Retrying safely](public-api-idempotency.md).

## Listing and filtering

- `limit` and `offset` page the list. The server caps `limit`.
- `q` searches; `sort` orders.
- Any other parameter that names a field is an exact filter, for example
  `?status=Draft&supplier=SUP-001`. A field the key cannot see is refused
  with 422, never silently ignored.

## Keeping another system in sync

Ask only for what changed:

```
GET /api/public/v1/records/Item?changed_since=2026-11-16T09:30:00Z&limit=200
```

If more rows are waiting, the answer carries an `X-Next-Cursor` header. Send it
back as `cursor=<value>` to get the next page, until the header no longer
comes. Save the time of your last full run and use it as `changed_since` next
time. A time that is not in the form `2026-11-16T09:30:00Z` is refused with 422.

## What a key cannot do

- Reach a module it was not given, or a document type outside its fine-tune list.
- Do more than its owner can, or anything once the owner is inactive.
- Approve, unless **Can approve** was ticked.
- Post money or close periods. Those stay on the screens.

A refusal carries `fix`, which says what to ask for, and `docs_url`. See
[Authenticating against the public API](public-api-authentication.md#when-you-are-refused).
