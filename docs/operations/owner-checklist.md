---
doc_id: DOC-OPS-OWNER-CHECKLIST
title: Owner checklist
type: procedure
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product-owner]
applies_to: source documentation; current working tree
authority: canonical
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-01-15
supersedes: none
superseded_by: none
---

# Owner checklist

Only you can do these. Everything else in the tracker is built by the team. Tick a box when it is done, then tell Claude "done: <line>". The numbers after each line are the tracker items it unblocks.

Never paste a key or password into chat. Put keys in the server's `/etc/erp/erp.env` or on the ERP's settings screen, as the line says.

## 1. Keys and accounts

- [ ] **Shopify:** make a free development store and give its Admin API access token. *26.2.1, 99.8.1, 146.5.1, 146.5.12*
- [ ] **BigCommerce:** give a sandbox store's API token. *26.2.1*
- [ ] **Magento:** give a test store's integration token. *26.2.1*
- [ ] **E-invoice (IRIS):** register on the IRIS sandbox as a taxpayer and as an API integrator, and get a test GSTIN. *75.2.0, 26.2.3, 75.2.11, 75.2.12, 75.2.13, 74.U.2*
- [ ] **E-way bill:** give the e-way bill sandbox login. *26.2.4, 74.4.1-74.4.7*
- [ ] **Pine Labs:** give the card-terminal sandbox credentials. *26.2.5*
- [ ] **Razorpay:** give the test keys (key id, key secret, webhook secret). Give live keys later, for client billing. *185.3, 50.12*
- [ ] **Client billing (after the Razorpay keys):** pick the client that bills the others and say its id (Claude sets `SAAS_BILLING_TENANT`). In Razorpay, add the webhook `/api/v1/integration/razorpay/subscription-webhook?tenant_id=<that client>` for the `subscription.*` events, and add a settlement bank account. Give your company's GSTIN for the invoices. *50.12*
- [ ] **Messages:** give a mail sender account, the DLT SMS registration (entity id, sender header, templates) and a WhatsApp Business account. *101.3.1*
- [ ] **Cloudflare:** give access to the `wholeops.in` zone, so the firewall can be switched on. *26.1.3b*
- [ ] **Alerts:** give a webhook address for alerts, for example a Slack or Teams channel hook. *26.2.2, 50.15*
- [ ] **Railway:** put the Railway token in `%USERPROFILE%\.railway\token.txt`. *134.11, 56.1, 56.2, 56.3, 56.5, 56.10, 56.13*
- [ ] **Unicommerce:** give the Uniware tenant name, an admin user, the facility codes and the shelf code, and the returns API pages (they are not public). *146.4.1*
- [ ] **Bank:** get the bank's statement API or SFTP login. *73.4.3*
- [ ] Once the keys above are in: one live order through a connector and one courier booking. *55.9*

## 2. Backups (5 minutes each)

- [ ] Make a private R2 bucket with a 35-day lock and a token for it, and put the values in `/etc/erp/erp.env`. *75.1.1, 75.1.7, 49.13.4*
- [ ] Turn on daily DigitalOcean backups for the server. *75.1.2*
- [ ] Save the `BACKUP_ENCRYPTION_KEY` and the `AUDIT_SIGNING_KEY` in your password manager. *75.1.3, 49.13.4*

## 3. Printers and shop PCs

- [ ] Jewellery sticker on the LD0801: print one sticker and one full row, send a photo. *183.1, 154.2, 189.1, 154.5*
- [ ] Y468BT: print **Test alignment** once, then one GRN label. *137.3, 143.6*
- [ ] On a roll, print one Square and one Ring jewellery tag and the four Seznik tags (one is 100x14.3). In the print dialog, check the paper size stays as set. *106.2, 113.3, 119.1, 119.4*
- [ ] If you have a Mac: print one label through PrintBridge. *31.1.9, 146.3.9*
- [ ] On a shop PC, run the new PrintBridge installer. Then quit it from the tray, restart the PC and check the tray icon is back. *137.4, 137.5, 137.6*
- [ ] One test print on the shop's TSC or Zebra through QZ Tray. *31.1.8, 58.7, 55.19, 106.3, 98.5.2*
- [ ] When app prints right, say "same on minn" and the printer setup is copied there. *154.4, 183.2*

## 4. Look at production (read only, 15 minutes)

- [ ] Run `scripts/audit_master_id_code_drift.sql` read only and send Claude the output file. *51.1a*
- [ ] For each live client, check **Select Industry** is set. *66.9*
- [ ] On minn, check every trading shop shows **Sellable = Yes**. *53.15*
- [ ] Check the reports module is on and the user-guide report steps work. *75.3.1*
- [ ] Allow `pg_stat_statements` on the server (Claude gives the one command). *47.9.3*
- [ ] Say "go" for Guide's read-only pass of every screen on app. Claude runs it. *132.4*
- [ ] Say "go" for Guide's module flows on app, in its test store only (the same flows that pass locally). *132.9, 132.12*
- [ ] Say "go" for read-only measurements on production (data growth forecast, cost of each security control, report load). *49.18, 49.18.1, 49.18.7, 26.10.7, 26.10.6, 47.18.2, 56.6*
- [ ] Say "go" for the reset on app and minn. An encrypted backup is taken first. *91.4*
- [ ] Say "go" to move production roles to the new role templates. Claude does it and keeps the before/after. *47.1*
- [ ] Say "go" to make the three database roles on the server ([row-level security review](../security/row-level-security-review.md)). *49.3.3*
- [ ] Say "go" to set the server clock to India's time servers (NIC / NPL). *47.16.3*

## 5. Say "switch on" after the next deploy

These ship in "report only" mode. After the quiet time below, say the word and they are switched to "enforce".

- [ ] Two-step login (MFA) by role, after two quiet weeks. *162.17*
- [ ] Cross-site write check, after one quiet week. *162.16*
- [ ] For the first week after a deploy, let Claude watch errors, speed and queues and write it up. *47.17.6*
- [ ] After a real client has run for a week, let Claude measure the background-job load. *176.7, 73.4.5*

## 6. People

- [ ] Write your phone number, a backup number and your lawyer's contact in `.local/escalation-contacts.md` (private, never committed). *49.12.1, 26.2.2, 49.17.3*
- [ ] Name CERT-In's point of contact for the business, then run one timed "report in six hours" drill with Claude. *47.16.3*
- [ ] Add a second admin, only needed for two-person approvals. *73.C.5, 49.14.5*
- [ ] Book real shop users for one test day. The run sheet is [uat-run-sheet.md](uat-run-sheet.md). *26.11.5, 55.22, 47.14, 50.13, 50.17, 55.0F, 135.9, 55.11, 74.U.1, 48.11.4, 50.12*
- [ ] Hire an outside security tester. The scope is ready in [pentest-scope.md](pentest-scope.md). They also test the live site through its proxy. *26.11.1, 20.5, 47.8.5, 49.10.7, 49.10.4, 49.4.6, 49.4.8, 49.17.7*
- [ ] Find one screen-reader user for an accessibility check. *47.16.6, 55.2*
- [ ] A 30-minute floor test with a shop phone and a barcode scanner (gloves, weak Wi-Fi). Scan one barcode with the phone camera at `/store/`. *47.6, 47.6.6, 49.14.6, 73.B.11, 102.1.2, 102.1.3, 102.5.2, 47.17.1*
- [ ] On a real till: turn the Wi-Fi off, make one sale, turn it on, check the bill number. *76.10*
- [ ] One HR walk on a real client: payroll, the team view, the holiday calendar. *55.14*
- [ ] Set up purchase orders yourself by following the [PO setup guide](../guides/po-setup-guide.md). *124.1*
- [ ] Load one real Amazon or Myntra category sheet into the marketplace templates. *146.5.2*
- [ ] Say "go" for one live Myntra product fetch. *34.4*
- [ ] On a client with an image drive: Settings > Integration, copy the photos to the drive once. *75.4.3*
- [ ] One walk as a real service user, and trace a project's cost from one posted journal. *55.18*
- [ ] minn: get the client's corrected file. *143.3*
- [ ] Name the first pilot client and its go-live date. *50.19, 74.5.7*
- [ ] Sign the hypercare plan, [§7](hypercare-plan.md). *26.11.6*
- [ ] Once a month: do the access and alerts review in [operating-cadence.md](../security/operating-cadence.md) (Claude can prepare it). *49.17.5*
- [ ] Where did you see "GRN 7"? Only three GRNs exist locally and none is number 7. *121.1*
- [ ] On the Data Console screen, press F12 and send Claude the red error line from the Console tab. *131.2*

## 7. Tidy up (2 minutes)

- [ ] Run `~/erp-scratch-archive/2026-11-14/drop-scratch-dbs-rest.ps1` and type YES. It drops the old test databases. *171.4*
- [ ] Run `~/erp-scratch-archive/2026-11-15/drop-scratch-dbs-ask193.ps1` and type YES. It drops the eight test databases this build used. *193.1*

## 8. Lawyer and CA

- [ ] **CA:** send the CA the [CA confirmation pack](ca-confirmation-pack.md) and record the signed answers. The pack includes the six new HSN rates and the e-way bill and job-work rules. Also ask the CA to look over payables, receivables and month close. *74.0.1, 74.5.7, 47.17.4, 55.10, 66.7*
- [ ] **Lawyer:** how long to keep personal data (DPDP). *47.16.2*
- [ ] **Lawyer:** who is responsible for card and payment data. *47.16.5*
- [ ] **Lawyer:** terms, privacy notice, data-processing agreement and support terms. Include what each package gives ([package entitlements](../product/package-entitlements.md)). *48.6.6, 49.16.1, 49.16.4, 50.12*
- [ ] **Lawyer:** the incident notice text, and whether any client outside India adds rules. *49.12.5, 47.16.1*

## 9. Drills and a spare machine

- [ ] Restore drill at a quiet hour, timed from the bucket (it proves the 4-hour restore target). *49.13.5, 49.13.4, 49.17.3*
- [ ] Ransomware drill on a clean, separate server. *49.13.6*
- [ ] A 30-minute "what if" incident talk, every quarter. *49.12.8*
- [ ] Emergency-update drill. *49.15.6*
- [ ] Rollback drill: put the old version back once and check the security-floor warning shows. *49.17.6*
- [ ] A spare server for the 1-million-row test, the 24-hour run and the many-clients busy-server test. *47.9.1, 47.9.5, 50.10, 50.12*
- [ ] A clean server image for the hardened-setup test. *49.7.8*

## 10. Say "release" and "deploy"

The next release and deploy prove the items below on their own. Claude runs both when you say so.

- [ ] Say "release" (the release test-runs the split test groups, the security gate and the attack-surface list). *177.1, 177.2, 177.3, 177.5, 47.17.2, 49.17.1, 129.5, 99.9.1, 47.9.4*
- [ ] Say "push" once. GitHub's checks then run the size budget, the guard tests and the fresh test runs. *177.4, 177.7, 50.11*
- [ ] Say "deploy". Then check the deploy log shows no warning for the alerts webhook. *109.12, 158.2*
- [ ] After the deploy, open **PIM > Workbench** on app and minn and check it loads. *149.2*
- [ ] After the deploy, add a passkey on **My Profile** with your phone or a security key, sign out, and sign in with it. *49.2.3*
- [ ] After the deploy, print one label of each printer type once (sticker, TSC, Zebra). *184.1*

## 11. When the screens are final

- [ ] Say "record the videos". Every SOP chapter is re-recorded once, then you review it and add a human voice. *57.12, 57.12a*

## Only when a client asks

Nothing to do now. Tell Claude when one of these is wanted, and it becomes a new item.

- [ ] A client uses trays: build license plates at receipt. *146.3.10*
- [ ] You want the text helper that sets a client up. *132.16*
- [ ] A client needs an extra module (treasury, PLM, field service and others). *50.18*

## Already decided, nothing to do

You approved every recommended answer on 2026-11-15 (ask 193). These were recorded:

- Optional extra modules (treasury, PLM, field service and others) wait until a named client needs one. *50.18*
- Staff security training and personnel controls start when a second person joins. *49.14.7, 49.14.8*
- No ISO or SOC certificate until a client contract asks for one. *49.16.5*
- You hold every approver role until a second person joins. *49.0*
- Client billing uses Razorpay ([ADR-009](../architecture/decisions/decision-register.md)). *50.12*
- Usage over a plan's quota is shown to the client, not charged. *50.12*
- Escalation table: you hold every role; numbers stay private ([incident runbook §2](incident-runbook.md)). *49.12.1*
