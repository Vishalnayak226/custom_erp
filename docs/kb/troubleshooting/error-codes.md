---
title: Reading an error code
section: Troubleshooting
order: 1
summary: What the three lines of an error dialog mean, and how to turn a code into an action.
audience: everyone
last_verified: 2026-08-12
---

# Reading an error code

Every refusal in this application carries a code such as `GLOBAL-0001` or
`SEC-0280`. The code is the fastest route to an answer, because it names the
exact condition rather than a category.

## The three lines of an error dialog

**Headline** - the catalog's own message for that code. It says what kind of
thing went wrong.

**Detail** - written by the engine that refused, naming the specific field,
record or value. This is almost always the line that tells you what to fix.

**Action** - what to do about it, from the catalog.

If you only read one line, read the detail.

## The code prefixes

| Prefix | Area |
|---|---|
| `GLOBAL-` | Validation, permissions, sessions, records |
| `SEC-` | Rate limits and security controls |
| `META-` | Document type and field definitions |
| `USERAC-` | Sign-in and account state |
| `REPORT-` | Report execution and export |

The complete list, with every message and its recommended action, is in the
generated [error code reference](../../guides/ERROR_CODES.md). It is produced
from the running catalog, so it cannot drift from what the application actually
shows you.

## The correlation id

Every error response carries a `correlation_id`, also shown in the dialog. It
identifies that one request in the server's logs. Quoting it in a support
request is the difference between "something failed yesterday" and a specific
line an administrator can look at.

## Common ones

**`GLOBAL-0011` Permission denied.** Your role does not have this permission.
Nothing you can do from your own account changes that; ask an administrator.

**`GLOBAL-0009` Session expired.** Sign in again. This also appears if your
account was deactivated or your role changed while you were signed in - the
server re-checks that on every request rather than trusting a token until it
expires.

**`SEC-0280` Rate limit exceeded.** You are sending requests faster than the
budget for that kind of request. The detail says which budget and how long to
wait.

**`GLOBAL-0004` Record not found.** Usually a record someone else deleted, or a
link pointing at something that no longer exists. Refresh and reselect.
