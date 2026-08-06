# Hypercare Plan — first pilot go-live

**Status: DRAFT for approval.** Written 2026-08-06 against checklist item 26.11.6.
Everything below is a proposal with a stated default; the four decisions in §1
are yours to confirm or change. Nothing here is in force until you sign §7.

Hypercare is the bounded period immediately after a pilot tenant starts doing
real work, during which response is deliberately faster and rollback
deliberately cheaper than steady-state operations. It ends on a date, not on a
feeling.

This plan **reuses** rather than restates:

- Severity definitions (P0/P1/P2) — [`incident_runbook.md`](incident_runbook.md) §1.
- Escalation contacts — [`incident_runbook.md`](incident_runbook.md) §2, still `TBD`
  (checklist 20.1). **Hypercare cannot start until those rows are filled in**;
  an on-call rota with no phone numbers is not a rota.
- Rollback mechanics — [`incident_runbook.md`](incident_runbook.md) §5.
- Backup/restore — [`backup_restore.md`](backup_restore.md).

---

## 1. The four decisions

| # | Decision | Proposed default | Why this default |
|---|---|---|---|
| 1 | **Duration** | **14 calendar days** from the first real transaction | Long enough to cover two weekly cycles (weekend trade, a Monday reconciliation, a month-boundary if timed well); short enough that the team doesn't silently live in hypercare forever. |
| 2 | **On-call owner** | One named primary, one named secondary, from `incident_runbook.md` §2 | This is a single-maintainer codebase today. The secondary exists so a lost phone or a flight isn't a single point of failure — the same reasoning behind Stage 32.5's MFA recovery codes. |
| 3 | **Coverage hours** | Business hours + 2h either side (08:00–20:00 IST), **not** 24/7 | The pilot is Indian retail; overnight write traffic should be near zero. 24/7 for one maintainer is theatre — it degrades daytime response, which is when incidents will actually occur. |
| 4 | **Rollback trigger** | Any P0, or two P1s inside 24h, or data integrity doubt (see §4) | Removes the judgement call from the worst moment. Pre-agreeing the trigger is the entire point. |

---

## 2. Response targets during hypercare

Tighter than the runbook's steady-state targets; they revert on exit.

| Severity | Acknowledge | First substantive update | Target resolution |
|---|---|---|---|
| **P0** — no safe workaround, live tenant blocked | 15 min | 30 min, then hourly | Rollback or fix within 4h |
| **P1** — major function broken, workaround exists | 1h | Every 4h | Same business day |
| **P2** — degraded/cosmetic | Next business day | Daily digest | Within the hypercare window |

"Acknowledge" means a human has seen it and said so — not that it's fixed.

---

## 3. Daily rhythm

**Every weekday morning (15 min, owner):**

1. **System Status screen** (Settings → System Status) — the Stage 26.1.2 screen.
   Check the warnings banner, last backup, last restore drill.
2. **Health + service state:**
   ```
   ssh root@<box> 'systemctl is-active erp caddy; curl -s localhost:8080/api/v1/health'
   ```
3. **Overnight errors** — Activity Log / `system_error_logs`, anything `PANIC` or
   `ERROR` since yesterday. `journalctl -u erp --since yesterday | grep -iE "panic|error"`.
4. **Backup actually ran** — not just that it was scheduled. `backup_restore.md`
   covers verifying the sha256 sidecar.

**Every Friday (30 min):** review the defect log (§5), decide whether the exit
criteria in §6 are trending toward being met, and confirm or revise the exit date.

---

## 4. Rollback triggers — pre-agreed, no debate at the time

Roll back (per `incident_runbook.md` §5) if **any** of these is true:

- **Any P0** that isn't understood *and* fixed within 2 hours. Understood is not
  enough; a known cause you can't fix still blocks the tenant.
- **Two P1s within 24 hours** — individually survivable, together a signal the
  build is not stable enough for real work.
- **Any doubt about data integrity.** Specifically: GL postings that don't
  balance, stock that doesn't reconcile against `inventory_availability`, or an
  approval that appears to have bypassed maker-checker. Roll back *first* and
  investigate from the backup — a corrupted ledger gets worse with every
  transaction written on top of it.
- **A failed nightly backup two nights running.** Operating without a recovery
  point is itself the incident.

**Not** a rollback trigger: cosmetic defects, a single slow report, one user's
browser cache serving a stale `app.js` (bump `?v=` instead — see 33.1.6).

### Rollback rehearsal — required before hypercare starts
The mechanism must be exercised *before* it's needed under pressure. `/opt/erp`
already carries timestamped `rollback_<UTC>/` directories from previous deploys,
so the artefacts exist; what has not been proven is a restore into a
production-like environment. That is checklist item **26.11.3**, and it is a
**precondition of this plan**, not a parallel task.

---

## 5. Defect log

One row per reported issue, kept for the whole window. This is also the
artefact 26.11.5's signed UAT closure log draws on.

| ID | Date/time | Reported by | Severity | Description | Root cause | Fix / commit | Closed |
|---|---|---|---|---|---|---|---|
| H-001 | | | | | | | |

Keep it wherever the team will actually update it. A stale log is worse than
none, because it gets trusted.

---

## 6. Exit criteria — all must hold

Hypercare ends when the date in §1 is reached **and**:

1. Zero open P0s, and no P1 open longer than 48h.
2. No rollback in the final 7 days.
3. Nightly backups succeeded 7 consecutive nights, with at least one verified
   restore (`restore_drill.sh` → logged in [`restore_drill_log.md`](restore_drill_log.md)).
4. The pilot tenant has completed at least one full business cycle end-to-end —
   for retail: purchase → GRN → stock → sale → return → reconciliation → period close.
5. The defect log has a decision on every row (fixed / accepted / deferred with an owner).

If the date arrives and these don't hold, **extend rather than declare victory** —
in one explicit, dated decision, not by drift.

---

## 7. Sign-off

| Role | Name | Date | Signature |
|---|---|---|---|
| On-call primary | | | |
| On-call secondary | | | |
| Business owner | | | |

**Hypercare start date:** ______   **Planned exit date:** ______

---

## 8. Known preconditions still open

Honest list of what must close before this plan can actually run. As of
2026-08-06:

| Precondition | Checklist item | Status |
|---|---|---|
| Real escalation contacts in the runbook | 20.1 | **Open** — parked, awaiting your input |
| Ops alert webhook (so alerts reach a human) | 20.2 / 26.2.2 | **Open** — parked, awaiting your input |
| DR/restore drill in a production-like environment | 26.11.3 | **Open** — now unblocked; the droplet is formalised as production (26.1.1) |
| Business UAT signed off | 26.11.5 | **Open** — run sheet drafted, needs real users |
| TLS on a real domain | 26.1.1 | **Open** — `enable_tls.sh` ready, awaiting a domain |

The first two are the hard blockers: without them an out-of-hours P0 reaches
nobody, which makes every response target above fiction.
