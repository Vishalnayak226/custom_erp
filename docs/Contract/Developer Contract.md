# Developer / Contractor Agreement (Template)

> **This is a starting-point template, not legal advice.** Fill in every `[bracket]`, then have a
> lawyer licensed in your jurisdiction review it before either party signs — especially the
> Governing Law, Non-Solicitation, and Liability sections, which vary significantly by country/state.
> Defaults below assume **India** (Indian Contract Act 1872, Copyright Act 1957, Information
> Technology Act 2000) since this product includes GST/e-invoice features — adjust if not applicable.

---

## Independent Contractor & Intellectual Property Assignment Agreement

This Agreement ("**Agreement**") is made on **[Date]**, between:

- **[Company/Owner Legal Name]**, referred to as the "**Company**", and
- **[Developer Full Legal Name]**, an independent contractor, referred to as the "**Developer**".

Together, the "**Parties**".

### 1. Background

The Company owns and operates a proprietary ERP software product (the "**Software**", currently
maintained in a private source-control repository referred to as the "**Repository**"). The Company
wishes to engage the Developer to maintain, extend, and/or support the Software. The Developer will,
in the course of this engagement, be given access to the Repository, its source code, architecture,
business logic, and related confidential information.

### 2. Scope of Engagement

2.1 The Developer will perform the following services (the "**Services**"): `[e.g., bug fixes,
feature development, maintenance, deployment support — describe scope, or attach as Schedule A]`.

2.2 The Developer will report to **[name/role]** and follow the Company's existing development
conventions, coding standards, and review process as documented in the Repository (including its
`CLAUDE.md` / contributor guidelines, where applicable).

2.3 Engagement type: ☐ Full-time employee ☐ Part-time/contract ☐ Freelance/project-based
*(strike out as applicable — this template assumes an independent contractor relationship; if
hiring as an employee, local labor law additionally applies and should be reviewed separately)*.

### 3. Compensation

3.1 The Company will pay the Developer **[amount/rate — hourly, monthly retainer, or per-milestone]**,
payable **[frequency]**, against **[invoice / timesheet]**.

3.2 Applicable taxes (GST, TDS, etc.) are the responsibility of **[party]** as required by law.

### 4. Intellectual Property Assignment

4.1 **Work Product.** All source code, scripts, documentation, designs, database schemas,
configurations, and any other work created by the Developer under this Agreement (the "**Work
Product**") is a **"work made for hire"** to the fullest extent permitted by law, and to the extent
it is not automatically so, the Developer hereby **irrevocably assigns** all right, title, and
interest — including all copyright, patent, and other intellectual property rights, worldwide — in
the Work Product to the Company, effective upon creation.

4.2 **Pre-existing IP.** Any code, library, or tool the Developer brings into the project that they
already owned or licensed before this Agreement ("**Background IP**") must be disclosed in writing
(see Schedule B) before use. Background IP remains the Developer's property, but the Developer grants
the Company a perpetual, irrevocable, royalty-free license to use it as part of the Software.

4.3 **Moral rights waiver.** The Developer waives, to the extent legally possible, all moral rights
in the Work Product (rights of attribution, integrity, etc.).

4.4 **No retained copies.** The Developer will not retain, republish, reuse, or incorporate the
Work Product (or the pre-existing Repository code they were given access to) into any other project,
client engagement, personal portfolio, or public repository, without the Company's prior written
consent.

4.5 **Assistance.** The Developer will execute any further documents reasonably needed to perfect
the Company's ownership of the Work Product (e.g., for copyright registration), both during and
after the engagement.

### 5. Confidentiality (NDA)

5.1 "**Confidential Information**" means the Software's source code, architecture, business logic,
customer/tenant data, credentials, pricing, roadmaps, and any other non-public information disclosed
to the Developer in the course of this engagement — including everything visible in the Repository,
regardless of whether it is separately marked "confidential".

5.2 The Developer will:
  - use Confidential Information only to perform the Services;
  - not disclose it to any third party without prior written consent;
  - protect it with at least the same care used for their own confidential information, and no less
    than reasonable care;
  - not copy or export it outside Company-approved systems (see §6.4).

5.3 This obligation survives termination of this Agreement **indefinitely** for source code and
trade secrets, and for **[3–5 years]** for other business information, except information that (a)
becomes public through no fault of the Developer, (b) was already lawfully known to the Developer,
or (c) must be disclosed by law (with prompt notice to the Company where legally permitted).

### 6. Repository & System Access

6.1 The Developer will be granted access to the Repository (GitHub) and any related systems
(servers, databases, deployment tools) strictly on a **least-privilege** basis — only what is
needed to perform the Services — and only under the Developer's own named account (2FA required,
no shared/generic logins).

6.2 The Developer will not add other collaborators, change repository visibility, alter branch
protection rules, or grant themselves elevated permissions without the Company's written approval.

6.3 The Developer will not commit secrets, credentials, or production data to the Repository, and
will use the Company's designated secrets-management approach for any credentials required.

6.4 The Developer will not clone or store the Repository or any Confidential Information on
personal devices, personal cloud storage, or personal accounts beyond what is operationally
necessary for active work, and will not retain such copies once access is revoked (§8).

6.5 The Company may monitor repository/audit logs for access and activity related to this
engagement.

### 7. Non-Solicitation

7.1 During the engagement and for **[6–12 months]** after its end, the Developer will not solicit
or attempt to hire the Company's employees or contractors, nor solicit the Company's clients for a
directly competing product, using knowledge gained through this engagement.

> **Note:** a blanket non-compete (e.g., "will not work on any ERP software for anyone else") is
> largely **unenforceable in India** under Section 27 of the Indian Contract Act, 1872, once the
> engagement ends. A narrower non-solicitation + confidentiality combination (as above) is the
> enforceable substitute — don't rely on a broad non-compete clause if this is under Indian law.

### 8. Term, Termination & Offboarding

8.1 This Agreement begins on **[start date]** and continues until terminated by either Party with
**[e.g., 15/30 days]** written notice, or immediately by the Company for cause (breach of
confidentiality, IP terms, or unsatisfactory performance).

8.2 On termination, within **[3 business days]**, the Developer will:
  - return or permanently delete all copies of the Repository, Confidential Information, and any
    Company credentials from all personal devices/accounts, and confirm this in writing;
  - hand over any work in progress, documentation, and credentials created during the engagement;
  - cease all access — the Company will revoke repository/org access, rotate any shared secrets
    (API keys, DB passwords, deploy keys) the Developer had access to, and remove 2FA/SSH keys
    tied to the Developer, on the same day access ends.

8.3 Sections 4 (IP Assignment), 5 (Confidentiality), 7 (Non-Solicitation), and 9 (Warranties)
survive termination.

### 9. Warranties

The Developer warrants that: (a) the Work Product is their own original work (or properly licensed
Background IP disclosed under §4.2) and does not infringe any third party's IP; (b) it does not
knowingly introduce security vulnerabilities, backdoors, or malicious code; (c) they have the legal
right to enter into this Agreement.

### 10. Liability

`[Placeholder — define a liability cap and carve-outs (e.g., no cap for IP/confidentiality
breaches) with legal counsel; varies significantly by deal size and jurisdiction.]`

### 11. Governing Law & Dispute Resolution

This Agreement is governed by the laws of **[India / State — or applicable jurisdiction]**.
Disputes will first be attempted to be resolved by good-faith negotiation, then by
**[arbitration under the Arbitration and Conciliation Act, 1996 / courts of [city]]**.

### 12. General

- **Entire Agreement** — this document (with its Schedules) supersedes all prior discussions.
- **Severability** — if a clause is unenforceable, the rest of the Agreement remains in effect.
- **No assignment** by the Developer without the Company's consent.
- **Notices** to be sent to the addresses/emails listed in the signature block below.

---

### Signatures

| | Company | Developer |
|---|---|---|
| Name | [Name] | [Name] |
| Title | [Title] | Independent Contractor |
| Date | [Date] | [Date] |
| Signature | ___________________ | ___________________ |

---

### Schedule A — Scope of Work
`[List specific modules/tasks, e.g., "maintain Manufacturing, HR, GRN modules; on-call for
production bugs; deploy via existing promote.ps1 pipeline."]`

### Schedule B — Developer's Disclosed Background IP (if any)
`[List any pre-existing code/libraries the Developer brings in, or write "None".]`

---

*Template last drafted: 2026-07-22. Re-review periodically and whenever engagement terms change
materially (scope, payment structure, or jurisdiction).*
