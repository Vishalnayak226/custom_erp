---
title: Incident Response & Alerting
section: Admin & Operations
order: 20
summary: Severity levels, what pages you automatically, where to look, and exactly what to run to roll a bad change back.
audience: admin
last_verified: 2026-09-03
screens: [system-status]
---

# Incident Response & Alerting

Most of what makes an incident manageable is deciding, in advance, how bad
it is and where to look — not inventing a process while something is on
fire. This page is that decision made ahead of time, plus the one piece of
it (automated alerting) this application actually does for you.

## Severity levels

| Level | Definition | Examples | Response target |
|---|---|---|---|
| **P0** | The live environment is down, or there is a data-integrity risk (wrong GL postings, corrupted stock, lost orders) | The server is unreachable; the database is down; a bug is posting unbalanced GL entries | Acknowledge within 15 minutes, begin mitigation immediately |
| **P1** | A major function is broken with no safe workaround | Checkout/POS billing failing; login broken; a whole module returning server errors | Acknowledge within 1 hour, fix or roll back the same day |
| **P2** | Degraded but a workaround exists, or a non-production environment is down | A report is wrong but not GL-affecting; one channel connector failing while others work | Fix within the current work cycle |
| **P3** | Cosmetic or low-impact | A UI copy/label issue | Backlog, no fixed response time |

Severity is set by whoever first notices the incident, using judgement
against the table — do not wait for a stricter threshold before acting on
something that is obviously a P0.

**Fill in your own escalation contacts before you need them**: who is
primary on-call, who is the secondary/escalation contact, and who signs off
on P0/P1 communications. This application ships with no opinion on who your
team is — that table is yours to keep current, not something the software
can populate for you.

## Automated alerting

Four things page you automatically, if you configure a destination for
them — deliberately not everything the system logs, since that would page
you constantly for routine single-request failures rather than the handful
of things that are genuinely urgent:

1. **A panic was recovered from.** Every crash the server catches and keeps
   running past alerts immediately, one alert per occurrence.
2. **A scheduled backup failed.** Names which stage failed (the dump, the
   encryption, or the restore step it's paired with) but deliberately never
   the command's own arguments, since those can carry credentials.
3. **A backup is missing or stale.** Checked hourly — the newest backup file
   must exist and be under 36 hours old. This is the trigger that catches
   what a failed-run alert cannot: a backup job that was never scheduled, or
   never actually fired, produces no failed run to report at all. Two
   consecutive missed nights is treated as serious enough to warrant rolling
   back a recent change, not just investigating it.
4. **A sustained error rate.** If one tenant logs 20 or more errors (of any
   severity) within a rolling 5-minute window, you get one alert, then a
   5-minute cooldown before the same tenant can page again — so a
   stuck-broken tenant pages you once per window, not once per second.

**What actually gets sent**: severity, source, and a truncated message only
— never a full stack trace or request body, since the alert leaves this
process for a third-party webhook. Every alert carries a correlation id, and
the full detail behind it stays in this system's own error log, one lookup
away.

**To turn alerting on**, point it at a Slack or Microsoft Teams incoming
webhook URL via configuration — until you do, alerts are recorded locally
only and nothing is sent anywhere. This is the one piece of incident
response only you can supply: the mechanism is built and already verified,
it just needs a real destination.

## Where to look

| What | Where |
|---|---|
| Live error/panic detail | The system error log, per tenant — query it directly, or through the **System Status** screen |
| A document's change/approval history | The audit trail, per tenant |
| Deployment history | The **System Status** screen shows the most recent deployment per environment |
| Correlation id | Every error response and log row carries one — use it to jump straight from a reported error to its exact server-side detail |

## Rolling back

**A bad promotion to a live environment:** roll back to the last recorded
good deployment for that environment, which also records the rollback
itself as a new deployment entry — so the deployment history never has a
gap explaining what happened.

**A bad database change**, only after the application-level rollback above,
and only if the bad change also wrote bad data: stop that environment,
restore its most recent known-good backup (see
[Backup & Restore](backup-and-restore.md) for the full restore procedure and
its confirmation safety gate), then start it again.

**A specific bad change already merged to the main branch:** revert it
explicitly rather than resetting history — this is a shared repository, and
a hard reset can discard someone else's in-progress work — then promote the
revert through the normal environment sequence like any other change, rather
than hand-editing a running environment directly.

## Troubleshooting

**Nothing pages when something clearly went wrong.** Alerting is opt-in —
confirm a webhook destination is actually configured. Until it is, incidents
still get logged locally; they just don't reach anyone automatically.

**An alert doesn't say what actually broke.** That's deliberate — alerts
carry a correlation id and a short message, not a full trace, since the
payload leaves the process. Use the correlation id to look up the full
detail in the system error log.

**A rollback needs to also undo bad data, not just bad code.** Do the
application-level rollback first, then follow
[Backup & Restore](backup-and-restore.md)'s restore procedure — don't do
these in the other order, or the newly-rolled-back code can immediately
write more bad data on top of a freshly-restored database.