# Documentation Index

Where to look, by question.

## "What is this system, in full?"
- **[ERP_BLUEPRINT.md](ERP_BLUEPRINT.md)** — a complete project snapshot for an outside reader (including an AI reviewer) with no other context: scope, architecture, build history, known gaps. Start here.

## "What's being worked on / what's left to do?"
- **[micro_checklist.md](micro_checklist.md)** — the live backlog, Stage by Stage, `[ ]`/`[x]` per item. Current source of truth for what's built.
- **[project_ledger.md](project_ledger.md)** — chronological build-record narrative, one section per Stage. Points back to `micro_checklist.md` for detail rather than duplicating it.
- **[ai_handover.md](ai_handover.md)** — handover snapshot for the next AI/dev session: environment setup, port map, run commands, latest commit, known concurrent-session risk. Read this first if you're picking up development.

These three ("the big 3") stay at `docs/` root by standing convention (see the repo's own `CLAUDE.md`) and are kept in sync after every unit of work.

## "I need to explain the business case / product scope"
- **[requirements/BRD.md](requirements/BRD.md)** — Business Requirements Document: goals, target market, stakeholders, scope, success criteria.
- **[requirements/PRD.md](requirements/PRD.md)** — Product Requirements Document: functional module inventory, user roles, workflows, built-vs-planned status.

## "I'm a new user / need to operate this system"
- **[guides/USER_GUIDE.md](guides/USER_GUIDE.md)** — client-facing, plain-language walkthrough. No jargon.
- **[guides/ADMIN_GUIDE.md](guides/ADMIN_GUIDE.md)** — standalone operator manual, zero AI assistance assumed: setup from a bare machine through day-to-day operation, deployment, and incident response.

## "How is this actually built?"
- **[architecture/framework_architecture.md](architecture/framework_architecture.md)** — the metadata-driven DocType kernel.
- **[architecture/architecture_evaluation.md](architecture/architecture_evaluation.md)** — stack choice and multi-tenancy rationale.
- **[architecture/pos_architecture.md](architecture/pos_architecture.md)** — POS design (mostly forward-looking spec, see its own status banner).

## "What was originally specified vs. what's actually built?"
Everything in `specs/` mixes real, built functionality with forward-looking design — each file carries its own status banner stating exactly which parts are which.
- **[specs/implementation_plan.md](specs/implementation_plan.md)** — the master technical spec (GL mappings, validation rules, API shapes).
- **[specs/modules_overview.md](specs/modules_overview.md)** — the full functional module directory.
- **[specs/industry_plugs.md](specs/industry_plugs.md)** — the multi-industry configurator spec.
- **[specs/pdf_blueprint_gap_analysis.md](specs/pdf_blueprint_gap_analysis.md)** — a 2026-07-12 snapshot audit against the original spec PDFs; superseded, historical record only.

## "How do I run/back up/deploy/respond to an incident?"
- **[operations/backup_restore.md](operations/backup_restore.md)** — backup/restore procedure and drill record.
- **[operations/incident_runbook.md](operations/incident_runbook.md)** — severity levels, escalation, rollback, log locations, alerting.
- **[operations/connector_live_verification.md](operations/connector_live_verification.md)** — verifying a real Shopify/BigCommerce/Magento connector against a live store.
- **[operations/hardening_roadmap.md](operations/hardening_roadmap.md)** — closed security/correctness hardening backlog; historical record only.

## Layout note
This structure (2026-07-19) groups docs by purpose rather than leaving 15+ files flat in `docs/`. Nothing was deleted in the reorganization — every file that existed before still exists, just under a subfolder. See `micro_checklist.md`'s Stage 19 entry for the restructuring record.
