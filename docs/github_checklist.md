# GitHub Security & Access Checklist

A running checklist for keeping the ERP's GitHub repository/org locked down as more people
(developers, contractors) get access to it. Unlike `micro_checklist.md` (feature build tracker),
this is an **operational security checklist** — append new items as new risks/tools come up, and
check items off only once actually verified on the live GitHub repo/org (not just planned).

> Related: see [`Contract/Developer Contract.md`](Contract/Developer%20Contract.md) for the
> legal/contractual side (IP assignment, NDA, offboarding) — this file only covers the technical
> GitHub-side controls. Neither one alone is sufficient; access control limits exposure, the
> contract covers what happens if it's copied anyway.

---

## Repository & Organization Setup

- [ ] Repository moved to / created under a **GitHub Organization** (not a personal account) so
      role-based permissions and audit logs are available.
- [ ] Repository visibility set to **Private**.
- [ ] Org-wide **2FA enforcement** enabled (Settings → Authentication security).
- [ ] Org billing/owner role limited to trusted account(s) only.

## Access Control (per collaborator)

- [ ] Each collaborator/contractor has their **own named account** — no shared logins.
- [ ] Roles assigned on **least privilege**: `Write` for developers doing day-to-day work,
      `Read` for reviewers/stakeholders, `Admin`/`Owner` reserved for the owner only.
- [ ] No collaborator has been given blanket **Admin** access without a specific reason logged here.
- [ ] External contractors added as **Outside Collaborators** scoped to this one repo, not full
      org members with visibility into other repos.

## Branch Protection

- [ ] Branch protection rule active on `main` (Settings → Branches).
- [ ] **Require pull request before merging** enabled.
- [ ] **Require approvals** (at least 1 reviewer) before merge.
- [ ] **Block force pushes** to `main`.
- [ ] **Block branch deletion** for `main`.
- [ ] (Optional, once CI exists) require status checks to pass before merge.

## Secrets & Credentials

- [ ] No API keys, DB passwords, JWT secrets, or `.env` files committed to the repo (spot-check
      with `git log -p` / secret-scanning, not just current HEAD).
- [ ] GitHub **secret scanning** + **push protection** enabled (Settings → Code security).
- [ ] Production database credentials and `DATABASE_URL` are **not** shared with contractors who
      only need repo/code access — use a separate staging/dev environment for their work.
- [ ] Any Personal Access Tokens issued are **fine-grained** and scoped to this repo only, with an
      expiry date set (not "no expiration").
- [ ] Deploy keys (if used) are read-only unless a collaborator specifically needs deploy rights.

## Monitoring

- [ ] Org **audit log** reviewed periodically (who accessed/cloned what) — requires GitHub Team/
      Enterprise plan; note current plan tier here: `[plan]`.
- [ ] Notifications/alerts on new collaborator additions or permission changes reviewed by the
      owner, not left silent.

## Offboarding (run this whenever an engagement ends)

- [ ] Collaborator/contractor **removed from the org or repo** the same day access ends.
- [ ] Any shared secrets they had access to (DB passwords, API keys, deploy keys) **rotated**.
- [ ] Their SSH keys / Personal Access Tokens tied to this repo **revoked**.
- [ ] Confirmed in writing (per the Developer Contract's offboarding clause) that local clones/
      copies have been deleted.

---

## Log

*(Append dated notes here as items are checked off or new risks are identified — e.g. "2026-07-22:
repo still on personal account, migration to org pending.")*

- 2026-07-22: Checklist created. Initial state not yet audited against the live repo/org — treat
  all items above as `[ ]` until verified.
