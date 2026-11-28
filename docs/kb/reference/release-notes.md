---
title: Release notes
section: Reference
order: 25
summary: What shipped and when, generated from the build ledger so this page can't say something the ledger doesn't.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Release notes
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Release notes

Generated from `docs/project_ledger.md`'s own Stage sections - **356** entries as of this build. Each excerpt is that section's own opening paragraph, not a rewritten summary, so it reads like an engineering build log because that is what it is. For the full detail behind any entry, including what was verified and how, read the ledger itself.

Newest first. Earlier entries continue on:

- [Part 2](release-notes-2.md) - 2026-09-09 to 2026-10-22
- [Part 3](release-notes-3.md) - 2026-07-27 to 2026-09-08

## 2026-11-28

**"build next 20 untouched items to build" - 13 defects found with Guide on a local copy of HEAD; 11 fixed, 2 wait on the owner** *(engines/ + public/ + admin guide, ask 319; 319.1-319.13; not committed)*

**"start building all of above" - import lookups read the key; an import survives a crash at every step and one real kill** *(engines/ + cmd/journeylab/ + guides, ask 317; 313.2, 313.3, 313.3.1; not committed)*

**"All recommended approved, build 297.5 and 297.8" - the report catalog answers 304, and journeylab sizes the server for 1 lakh users** *(internal/server/ + public/ + cmd/journeylab/ + guides, ask 318; 297.5, 297.8; not committed)*

**"Need a UI where store wh can request in in ui & accept order & proces & dispatch it. or auto trigger" / "Order means inventory request for any case, I need submenu of request reason or can be text like advance order..." - stock requests between locations and a sourcing engine** *(engines + server + public + guides, ask 324; 324.1-324.4; not committed)*

**"Now we have plan throughly the inv planning. I have a recomendation & books to gather knowledge & build this." - the inventory planning plan** *(docs only, ask 323; 323.1-323.30)*

**"How to avoid this kind of things? clash" / "Fix it" - a sidebar menu and a page's drop-down list are never open together** *(public/app.js + user guide, ask 326; 326.1; not committed)*

**"see if anything helpful to build" / "Build it" - a reel's 19-point security list, checked against the ERP; the one gap, a guard on SQL built from text, is built** *(internal/securityscan/ + docs, ask 322; 322.1; not committed)*

**"1 click/ meessage it will do all steps" / "It will do you waant print. yes will print" / "buld that" - the bulk PO upload sends the stock on to a store and offers the stickers** *(internal/server/ + engines/ + public/ + guides, ask 320; 320.1-320.2; not committed)*

**"pim has 5 open itm cabe built" - PIM's open items are built; Shopify now drops a removed recommendation list** *(code + guides, ask 321; 321.1)*

**"See the latest image why there is a gap in sequnce of comb id" / "Bulk upload is not working in Min properly ... It should accept and auto fix in erp" - the upload reads bills as a person would** *(internal/server/ + public/ + guides, asks 315 and 316; 315.1, 316.1-316.4; not committed)*

**"Read all and plan below problem statement" - the scaling and recovery plan** *(docs only, ask 313; 313.1-313.4, refines 297.5 and 297.8)*

**"Can you learn something for erp" - Zerodha's Blobasaur, checked against the ERP: nothing to copy** *(docs only, ask 314; 314.1)*

## 2026-11-27

**"approved" - Security Events is five pages under Settings › Monitoring** *(public/ + guides, ask 311; 311.1-311.2, closes 288.3; not committed)*

**"fix it  what is open from above" - a list the user leaves is not a server error; point-in-time recovery still waits for the owner** *(internal/server/ + admin guide, ask 312; not committed)*

**"Analyse the telegram folder and see why min is faciling issue" / "Fix it from here" / "read the log of error for min and see how to fix from here" / "Set up everything from here" - minn's stickers go to the store PC's DP27** *(minn data + public/, ask 310; 310.1-310.2)*

**"Once all activity done, push all release all. Wait for all developement to finish" - 0.30.0 and 0.31.0 live on app and minn** *(release/ + internal/server/ test, ask 308; 308.1)*

**"Is the printer page is better/best for increasing and zoom in and config wise..." / "Printer will be handed over. All should be checked and closed." / "Text style, I need world class" - the LD0801 round shipped and closed, all but the real print** *(public/ + release/ + Guide, ask 289 with ask 286; 286.1-286.3, 289.1-289.3, 275.2)*

**"Is the erp is lightweight for browser? It should work in very kbps speed of internet... If User bulk upload 1Cr row also it should not fail..." / "All recommended approved, build all" - no hard refresh after a deploy, the 2G profile, the tab's footprint, a Tasks bar for long work, imports of any size** *(engines/ + internal/server/ + public/ + db/migrations/ + release/ + guides, ask 297; 297.1/297.3/297.6/297.7 in release 0.30.0, 297.2 in release 0.31.0)*

**"What is open, no one is working start building next 10 itesm" - the Stores screen, print logs on local days, empty-list text cut, CRM Ready to send as its own page** *(engines/ + public/ + guides + KB, ask 309; not committed)*

**"Paid algorithms course ($5); its terms forbid reuse- Is it required? I can get a book" / "& Above each and everypoint approved build it" - point-in-time recovery, the version check on every edit, and "How it works" pages in Help** *(deploy/ + engines/ + internal/server/ + public/ + KB + guides, ask 305; not committed)*

**"Fix open items" - what ask 298 left open: a stale dashboard load, the emailed dashboard's read check and % format, the code map** *(engines/ + internal/server/ + public/ + guides, ask 307; not committed)*

**"Not alllowing me to create new product type" / "I can't any product detail. I want to see everything expaned design number, comb id , color and all attrubute price" - a picker offers "+ Create" beside close matches, and the item grid keeps room for its lines** *(public/ + guides, asks 303 and 304; not committed)*

**"I need proper dashbaord for each sales and all" / "Dasbord with number and visulation. It need control to whom it will be show at role level." - four ready-made sales dashboards on one all-channel dataset, and a role choice for every ready-made dashboard** *(engines/ + internal/server/ + public/ + guides, ask 298; not committed)*

**"This is not looking good this hyper linke in PIMs make better" - PIM Home's setup path as numbered step tiles** *(public/, ask 302; not committed)*

**"All recomened" - Home first in every module with tiles or queues, one job per page in Expenses and PIM My Work, and "one page, one job" in the constitution** *(public/ + guides + KB + CONTRIBUTING.md, ask 300 on ask 284; not committed)*

**"Guide walk me through PO to bill. I will record it also." / "I need store master build that." - Guide's live purchase-to-sale walk on app, and a plan for a Stores screen** *(Guide in ~/dev-tools/guide + tracker, asks 295 and 301; not committed)*

**"No dropdown should show like this. Go rhrough each page guide and tell them and If you can do with out guide also fine." - every suggestion box uses the app's own menu, not the browser's black popup** *(public/ + store app + user guide, ask 294; not committed)*

**"How to create offer group?I should be naviagted from here. & Each naviagtion should open new tab right?" - the offer form links to the Product Group list in a new tab** *(public/ + admin guide + Guide, ask 296; not committed)*

## 2026-11-26

**"Build all of above" - columns each user chooses, one-line rows with a ⋯ menu, short empty lists, one type scale and Home tiles** *(lists + shell + styles + guides, ask 280; not committed)*

**"Each and every page should behave like a river ... 10% to 1000%. No issue" - summary tiles keep words whole at any zoom; the fit suite now catches a broken word** *(public/ CSS + JS + Guide, ask 287; not committed)*

**"Switch MFA to Report on app, write it to minn" - two-factor for sensitive actions now reports on both clients** *(production setting, ask 293; no code)*

**"See what is usefule. Keep knowledge. We can use or build bring it" (three learning sites) - system design lessons kept, two gaps found** *(docs + tracker, ask 290; not committed)*

**"the alert URL ... the offsite storage credentials and the second copies of the key files; the devices for 267.5; running the script that drops the 4 scratch databases; Build above" / "MFA ... Why required? Why disabled?" - scratch databases dropped; the rest needs the owner's accounts, devices or word** *(local Postgres + tracker, ask 291; not committed)*

**"Can erp be made more lightweight and optimized" / "All recommended approved, build all and don't loose feature." - comments stripped as served, lighter idle workers, abandoned queries stop in Postgres** *(server + workers + db driver + guides, asks 283 and 285; not committed)*

**"See telegram. Print quality is not premium. Make it more premium ... It should left aligned barcode text can be center. MRP and Item alias swap. & The Right side text alos one alignement" - a darker, crisper Seznik tag on one grid, and a way for saved templates to take it** *(sticker engine + studio + guides, ask 286; not committed)*

**"OMS home required. Order will show only order. Its for all module. One page will have only one pages required field nothing else" - OMS Home, Orders as a plain list, PIM task counts on the PIM Dashboard** *(screens + guides, ask 284; not committed)*

**"Healer done. Print stikcer from app" - one real sticker from app on the LD0801 after the Data Healer** *(Guide on app, ask 282; no code change)*

**"278.1 ... Recommended (a) ... 278.2 ... Recommended (a) ... Build approved" - a new client's Accountant from its template; HR Manager reads and writes pay fields** *(roles + sensitive fields, ask 279; not committed)*

**"Anything open which is buildable" / "Which are not build being build by other session" - open list checked; nothing buildable left without the owner, two items closed from GitHub evidence** *(tracker only, ask 281; not committed)*

**"Built all of above approved all" - the exchange-price guard, production evidence and the cross-origin switch, and every Guide journey run by its own roles** *(returns engine + production + Guide, ask 278; not committed)*

**"Guide walkthrough all screen and see if the all columns are visible properly in all the url" - column visibility and UI/UX walk of every screen on minn, findings filed for review** *(Guide on minn + QA record + tracker, ask 276; not committed)*

**"Data headel fix. Fix loading issue identified." - Data Healer's Item column no longer looked up as items; Guide pacing ruled out for the minn "Loading..."** *(public/view-data-healer.js + tracker, ask 277; not committed)*

**"Guide test agin and lets handover to minn owner" / "After feedback apply is not working" - minn's LD0801 at the new shift, and Fix from photo answers each press in its dialog** *(Guide on minn + print-check.js + guides, asks 270 and 275; not committed)*

**"All approved recommedation" - sales velocity counts Sales Invoices, less their returns** *(engines + guides, ask 274; not committed)*

**"Make the ERP great. Think out of box and see what can be imporved. Make best. Plan and implement." - move stock before buying, the last price on every PO line, a guard on selling below cost, and a faster velocity read** *(engines + planning/PO/till screens + guides, ask 273; not committed)*

**"Upload the PO in mine then minn" - the client's 2nd purchase file received and moved to the store, ready to bill, on app and minn** *(data only through Guide, ask 271; no code change)*

**"Allow billing in combination also. Search with design or combination and show barcode and select and do billl" - the till bills a combination found by its design no.** *(list search + availability + view-pos.js + guides, ask 272; not committed)*

## 2026-11-25

**"Billing and Shipping add will be same as TOP store selection" - a new PO's addresses start as the top-bar store** *(view-procurement.js + guides, ask 268)*

**"Push and deploy all if nothing is pending to build" - release 0.29.0 (9db21d3) deployed to app and minn** *(release scripts + securityscan + styles.css + one test, ask 265)*

## 2026-11-24

**"Why it is printing so much when i connected printer" - PrintBridge refuses a print to a printer that is off or unplugged instead of letting Windows hold it** *(PrintBridge agent 0.3.3 + public/downloads + guides, ask 249; not committed, held from release)*

**"Make it more smarter, not working" - Fix from photo reads a Line up, the alignment test or the label's own print, not only the check label** *(print-align.js + print-check.js + sticker-engine.js + tests + user and admin guides, ask 257; not committed)*

**"finish it" - the rest of 232.1: twelve KB pages reviewed against the app, the handover back under budget, and release G7 stops on what CI's doclint stops on** *(12 KB pages + handover + developer-setup + doclint message + release G7, ask 264; not committed)*

**"Rename all thos QZ as well" - no "qz" names left in the code; old names keep working where an install depends on them** *(renamed Go/JS files + routes + settings + one migration + one heal + guides + release gate + Guide tool, ask 263; not committed)*

**"Need multi tender & need Tender master to add" / "Need remarks" - split payment and Add tender in the till's Payment Mode box, and remarks on a bill** *(one handler + two engine files + three JS files + tests + user and admin guides, ask 260; not committed)*

**"Show only in one not non inv one" / "Show qty avl as well in the selctn" / "throw error if inv not avl" / "after loading/scanning" - the till sells only what is in stock here** *(engines + one handler + two JS files + test + user guide, ask 262; not committed)*

**"I need city Master after State selctuon. Else cane typed also. Samefor pincode after state can be typed also. Size not required. ..." - City and PIN lists on the customer form; a shorter till form** *(one JS file + shipped PIN directory + generator + guides, ask 261; not committed)*

## 2026-11-23

**"Alredy number typed don't ask number agin. Prepolulate nmber" - the till's New customer form starts with the number already typed** *(one JS file + user guide, ask 259; not committed)*

**"Why QZ is mentioned in checklist. No where QZ should be there as in code as we eliminated aqz from erp. if marketplce rqd then fine keep it" - QZ Tray taken out of everything a user reads** *(two JS files + one Go string + guides + KB + trackers, ask 258; not committed)*

**"so many are crossed in checklist and why present. Move to archive which is done" - every Done item moved out of the live tracker** *(trackers only, ask 256; not committed)*

**"Build all of above" - then "Add in above, Report and all transaction also should sync seemlessly. Means each table should be updated properly.": the lag and code-quality fixes built, and every multi-table write swept so stock, money and logs save together** *(engines + db + one migration + phone pick + guides, asks 252 and 253; not committed)*

**"Read through erp each code and see what can be lagging erp. I need code expert." - then "issue 1st, lighter weight ... proper error displayed in all error, module interconnection, never failing inventory management, smooth UI": a code review for lag and code quality, 8 findings filed, nothing changed** *(review only, asks 248 and 250; tracker only)*

**"Build next 20 items" - a second Guide bug hunt on the working tree: 4 real defects found and fixed** *(navigation + stickers + expense claims + OMS Ship + guides, ask 246; not committed)*

## 2026-11-22

**"Build next 20 items" - a Guide bug hunt on the working tree: 14 real defects found and fixed** *(reports + settings + WMS waves + CRM/HR/stock screens + list sweeps + guides, ask 245; not committed)*

**"Build next 20 items approved all and recommened all" - the phone view switch meets its budget, and the whole browser and server gate is met on the release evidence** *(Home screen + budgets file + trackers, ask 244; not committed)*

**"build next 20 open items", then "approved all" - Guide's write runs on app, PrintBridge down timed on the real agent and fixed to 9.5 s, a real Line up strip printed** *(qz-print.js + two printing guides + Guide suites, ask 243; not committed)*

## 2026-11-20

**"Start building next 20 items. all approved and all recommened" - phone first paint meets its budget, two reports and a screen verified on app, the PO setup guide checked, Guide's PIM run cleans up after itself** *(app shell + PO guide + Guide suites, ask 242; not committed)*

## 2026-11-19

**"Once all build done push and deploy" - 0.28.0 released and deployed** *(release fixes + guard list + tracker, ask 240; d6c4fa9 live on app + minn)*

**"Build next 10 item all appproved recomeded. Dont ask anything just build" - Trusted Types trial behind a flag, browser budgets and journeys made on GitHub for every candidate** *(server header + release gate + release.yml, ask 241; shipped in 0.28.0, the J5 fix not committed)*

**"fast it fast on processing all approved and all recommnded" - lists draw 20 rows first, a lighter phone boot, a faster first search key, G4 bumps every shell script** *(app shell + 3 screens + release script, ask 238; not committed)*

**"compete buildanble next 10 items" - a deleted GRN stops counting, the GRN MRP click, deleted records leave the reports** *(engines + GRN steps screen + guide, ask 239; not committed)*

## 2026-11-17

**"Fix 232.1 and approve the 205.24 write walk and Fix all the above issue and Fix the open items" - CI test fixes, the 205.24 walk, a Workbench bug, a faster G4c** *(tests + release scripts + PIM screen, ask 235; not committed)*

**"Fix all the issue" - each extra print checked to its own printer** *(Guide suite only, ask 236; no repo code change)*

**"Once they finish start working." - perf budgets re-baselined, two layout shifts fixed** *(app shell + CSS + ratchet, ask 233; not committed)*

**"Take necessary action. and build all approved all. release" - 0.27.0 released and deployed** *(release fixes + tracker, ask 232; 584eebd live on app + minn)*

**"Build all the pending above approved recommeded" - Share module API stays Admin only** *(test + KB, ask 228; not committed)*

**"Build next 40 open items" - API access verified end to end, nine bugs fixed** *(public API handlers + key engine + screens + tests, ask 226; not committed)*

**"Fix that as well" - module-route webhook events, APIKEY-0512, extra prints checked** *(middleware + engines + tests + Guide + guides, ask 227; not committed)*

## 2026-11-16

**"build 7" - a live print bug found and fixed, the till's print dialog and Guide's key verified** *(print rasteriser + release gate + Guide suites, ask 225; not committed)*

**"start building next 20 open items" - merged masters move inside line lists, the sticker manual checked against the app** *(master merge + guides, ask 223; not committed)*

**"fix bugs" - the till and scan fixes verified by the Guide journey** *(verification + Guide suite, ask 221; not committed)*

**Knowledge Center pictures get their own budget** *(docs generator + release budget + doclint + standard, ask 222; not committed)*

**"Start next all open next 20 items" - till fixes, B2B till bill IRN, report business day, wrong password keeps you signed in** *(POS + e-invoice + reports + sign-in, ask 220; not committed)*

**"Push all and deploy" - release 0.26.0, deployed to app and minn** *(release + deploy + cold-core and test fixes, ask 219)*

**Every transaction screen has its own lesson, and every module a picture** *(KB handbooks + KB build + guides + Guide suites, ask 217; released in 0.26.0)*

**Best-practice POS: B2B bill, GST cash/PAN rules, three discounts, every standard offer, Sales History, online bills and returns at the till** *(code + migration + tests + guides + Guide suites, ask 215; released in 0.26.0)*

**PrintBridge in its own public repository, the build signs it once a certificate exists, and Test alignment on paper** *(PrintBridge repo + build script + real print, ask 218; released in 0.26.0)*

**Data Healer rule in the constitution, and Guide's key set live** *(constitution + PR template + production config, ask 216; released in 0.26.0)*

## 2026-11-15

**Screens hold still when you type or click - every screen walked, all three roles** *(front end + guide + Guide suite, ask 200; released in 0.26.0)*

**Switch paper, template and printer for one print - at Print, the till and Reprint; profiles by B2B / B2C and user** *(code + migration + tests + guides, ask 213; released in 0.26.0)*

**Data Healer > Past names: change an item's name on documents already made, without SQL** *(code + tests + guides, ask 211 with 201.4; released in 0.26.0)*

**PrintBridge: "did not print" in seconds, no wake-box, and a security review - agent 0.3.2** *(ERP + PrintBridge agent + tests + guides, asks 209 and 212; released in 0.26.0)*

**Help opens the Knowledge Center at your screen's lesson: What, Why, Use case, How, with pictures** *(front end + KB + guides, ask 210; released in 0.26.0)*

**Data Healer: one-time fixes old records catch up through, starting with Item aliases** *(code + migration + tests + guides, ask 207; released in 0.26.0)*

**System Errors becomes a fix list: real faults only, one row per fault, Copy for fix and Resolve** *(code + migration + tests + guides, ask 206; released in 0.26.0)*

**Each screen's short code beside its name in the sidebar menus** *(front end + guides, ask 208; released in 0.26.0)*

**API access for every module, limited by module access - research and plan** *(plan only, ask 205; released in 0.26.0)*

**"Push all and deploy all" - release 0.25.0 once every session was done, deployed to app and minn** *(release + deploy + pipeline fixes, ask 204)*

**"Build everything. All approved" - every buildable open item: SaaS packages and billing, passkeys, tax rules from official sources, Unicommerce, the role checks Guide found, and the owner checklist** *(code + tests + guides + docs, ask 193; shipped in 0.25.0, §379)*

**OMS picks and packs by typed SKU without a barcode; Guide runs PO → GRN → transfer → bill on app** *(code + tests + guide + Guide suites, asks 197 and 198; released in 0.25.0, 53af64d)*

**A short item alias on stickers, the item name from Category + Type, and combinations named by their item name** *(code + migration + tests + guides, asks 199 and 201; released by 0.25.0)*

**A short code for every menu screen, and a switch to turn codes off for a client** *(code + tests + guides, ask 202; released in 0.25.0, 7c43c56)*

**"Build all recommended" - search on very large masters, the panic button off Activity Log, the cold-load gate re-run and three more connected-organisation journeys** *(code + tests + guides + docs, ask 192; NOT COMMITTED)*

**"start building all next 40 itms" + "all recomennded approved" - 37 buildable items: the Customer-create migration, CI that always tests, jobs capabilities, a strict style CSP, a second-replica job test, an API access page, and the doc and walk items** *(code + tests + guides + docs, ask 191; NOT COMMITTED)*

## 2026-11-14

**"Build above pending all approved" - what ask 178 left open: Forgot password from the sign-in screen, a second-admin document's own APPROV code, the ten doclint findings that stop release G7, 57.26's browser walk, and the approved dead-code list where its files were free** *(code + tests + guides + KB, session erp-0f, ask 186; NOT COMMITTED)*

**"Clean up" + "Build half and give me a prompt to build rest 50%(now)" - the first half of ask 187: the local dev data cleaned up, guard tests first, a faster test database on GitHub, a certificate check at deploy, a security-change acknowledgement at release, and every hook naming who it acts for** *(release scripts + CI + code + tests + SDK, session erp-e0, ask 187; NOT COMMITTED)*

**"Build half and give me a prompt to build rest 50%(now)" - the second half of ask 187: QA, UI, Docs and TEN items sorted into buildable, gated and decision; the re-code tool's guide paragraph, sections that keep their rows while they reload, a camelCase field-name fix, and the open decisions with a recommended answer** *(code + tests + guides, session erp-ba, ask 188; NOT COMMITTED)*

**"Build all untouched next 30 items" - PIM, PROC, INV, PRT, CHN, DATA and CFG: a media rule exception that a second person approves, master merges through approval, a registers step on the setup path, till number series for offline bills, one badge per setup-path step, and three decisions with a recommended answer** *(code + tests + migrations + guides, session erp-fa, ask 179; NOT COMMITTED)*

**"C is getting filled. See what we can clean up" - C: from 22.6 GB to 48.7 GB free** *(machine clean-up, no code, ask 181)*

**"I got the new printer, guide test in now" - the new label printer does not take TSPL** *(test only, no code, ask 182)*

**"all recommended" / "Build all don't ask/wait for approval" / "Complete al" - ask 175's decision batch built as recommended: the shipped bootstrap accounts closed, ten screen and save fixes, approval delegation and reminders for every document, the capability checks filed, and every stock ledger line keeping its UOM, cost, price and tax** *(code + tests + migrations + guides, session erp-70, ask 178; NOT COMMITTED)*

Detail is in the tracker under each item id and the ask 178 run header.

**"Recent not required in home. Home is for Dashboards Not shortcuts" - Home loses its Recent list and its quick-action tiles** *(code + guides, ask 180; NOT COMMITTED)*

Detail is in the tracker: the ask 180 run header in the Asks log and 180.1 in *RPT › Dashboards & home*.

**"Once all pushed and deployed start building all the open items" - FIN, TAX, HR, MFG, ASQ and RPT: the gaps left on ask 170, receivable and payable accounts by party group, a cash position, travel requests, corporate cards, field visits, help articles and instrument calibration, and every ask-170 item verified** *(code + tests + guides, session erp-57, ask 176; NOT COMMITTED)*

Detail is in the tracker under each item id (now in the archive) and the ask 176 run header.

**"Why it is taking so much time?" / "i need is 1 min all should be done" - the release suite moves to GitHub's machines and the click's own gates are cut** *(release scripts + CI + guides, session erp-5c, ask 177; NOT COMMITTED)*

Detail is in the tracker: 177.1-177.7 in *OPS › Release pipeline & gates* and the ask 177 run header.

**"Once al push and deployed start build" / "All open items" - TEN, SEC, PLAT, UI, OPS, QA and DOC: what 0.24.0 proves is closed, one decision batch with recommended answers, hidden fields kept off prints and filters, a consent-withdrawal test, and the budgets re-run on a quiet machine** *(code + tests + guides, session erp-70, ask 175; NOT COMMITTED)*

Detail is in the tracker under each item id and the ask 175 run header.

**"push & deploy all" - release 0.24.0 (bfd0ff0) and its deploy to app and minn** *(release + deploy, session erp-5c, ask 174)*

Detail is in the tracker: 174.1 in *OPS › Release pipeline & gates* and the ask 174 run header.

**"Start building next 20 item. Include from prev which was left in half way" - eight decisions built as recommended (a read-only mode, an encrypted evidence pack, buyer e-mails dropped after delivery, no takeover of an unrecorded command claim, the store cut on two old reports and the payroll export), the loyalty spend served by an index, PO and transfer-in line upload, security friction measured, a privacy-requests screen, and two more replayed commands in the E8 journey** *(code + tests + guides, session erp-2b, ask 173; NOT COMMITTED)*

Detail is in the tracker: the ask 173 run header in the Asks log, and each item named below in its sub-module.

## 2026-11-13

**"Start buildng all this. Carfully build I am building parallely other things" - every open FIN, TAX, HR, MFG, ASQ, RPT and PRT item: line-level ledger rows, bank part-matching and files, leases, CWIP, impairment and revaluation, maintenance cost and meters, production confirmations, scrap and WIP costing, lot genealogy, AQL sampling and supplier SCARs, positions, recruitment, exits, disciplinary cases, certificates, calibration, slab TDS / LWF / ECR / 24Q, benefits, salary revisions, a manager's team view, people analytics, holiday calendars, expense limits, staff advances, timesheets, milestones, project revenue, service SLAs, GSTR-1 / 2B files, export under LUT, retention policies, staff and vendor erasure, a masked sandbox copy, a data-quality queue and import load order** *(code + tests + guides, session erp-e9 / erp-d7, ask 170; NOT COMMITTED)*

Detail is in the tracker: the ask 170 run header in the Asks log and 170.1-170.43 in their sub-modules (FIN, TAX, HR, MFG, Assets, Quality, Data).

**"Use in optimal way where laptop is not getted heated up" / "this first priciple for building in the project" - building without overloading the machine becomes the constitution's first principle** *(docs + local settings only, session erp-71; NOT COMMITTED)*

Detail is in the tracker: the ask 172 run header in the Asks log and 172.1 in *OPS › Repository & developer tooling*.

**"Once all built mark done and keep in archive list" / "Clean up all temp file for the project" - the first archive pass, a disk clean-up, and both as rules after every task** *(docs + local machine only, session erp-0b; NOT COMMITTED)*

Detail is in the tracker: the ask 171 run header in the Asks log, 171.1-171.2 in *DOC › Engineering docs & trackers*, 171.3-171.5 in *OPS › Repository & developer tooling*.

**"Complete all this 3" - every open PIM, PROC and INV item: contracts and service POs, supplier onboarding and a supplier portal, product lifecycle, range, markdowns, merge, dated changes, listing drift, category attribute rules, the four procurement decisions, a Myntra price harvester, a photo copy into the image drive, and nine walk-throughs** *(code + guides, session erp-0b; NOT COMMITTED)*

Detail is in the tracker: the ask 168 run header in the Asks log, and each item in *PIM*, *PROC* and *INV*.

**"build all print modules open items" - bin labels through sticker templates, Sales Order as a sticker source, GRN quantity headings that no longer break, and the print items that only needed a live check walked on a scratch server** *(code + tests + guides, ask 167; NOT COMMITTED)*

What changes for existing clients: the Bin list and the Bin Generator have **Print labels**, which opens a dialog (template, copies, Print to the sticker printer, **A4 sheet** for the old print window) with a built-in *Bin label 50 × 25*; **Sales Order** is in the sticker screen's Module list; the built-in GRN print's quantity headings are 10 wide (the table total is still 114). No migration, no new route.

## 2026-11-12

**"Once deploymeent is done, Start next 20 items to build" - a stock ledger key index, cached setup checks, the synthetic client's PO step in batches and a 100k end-to-end run, a smaller `app.js`, reason-code tabs, the last three strict record types, journal voucher lines checked on every save, market-specific product values, a store move from the Users screen, favourites for every role, notification clock badges, erasure that reaches files and saved views, go-live evidence that goes stale, a replay journey, both guides split into parts** *(code + tests + guides, session erp-10, ask 166; NOT COMMITTED)*

What changes for existing clients: the Users screen says when a home-store move went for approval; the Setup reason-code list has category tabs; Security Events shows Due / Overdue on each notification decision and links the incident runbook; a role made in Setup can keep favourites; SetOffVoucher, StewardshipChange and ImportBatch refuse a status move with no rule; a JournalVoucher saved or imported through the generic route is refused when its lines do not balance (FIN-0844) and its total is set from its lines; an erasure also deletes the person's attachment files and saved views; go-live integration evidence goes stale when a destination is added or switched off. The admin Home card's Finance checks can take up to a minute to show a posting change. Both guides are now an index plus part files.

**"all recommended" - four decisions built: strict transitions for the last eight approval / WMS / service record types, a High-severity reason that holds a purchase return or a transfer loss for approval, signed public image addresses for marketplace feeds, a Privacy approver role** *(code + tests + guides, session erp-4c, ask 165; committed in 1c026d1, release 0.23.0)*

What changes for existing clients: PasswordResetRequest, SupplierSubmission, Appraisal, Grievance, LPN, SortSlot, ServiceTicket and Appointment refuse a status move with no rule (GLOBAL-0019), and rejecting one of the four approval records needs a reason; a purchase return or a transfer loss with a High-severity reason waits in Pending Approval; a new Privacy Approver role can be named on a notification decision. Public image addresses stay off until the owner sets `PUBLIC_MEDIA_SIGNING_KEY` and turns the setting on. Two migrations change metadata only (rules, a hold record type, one favourites grant); no client document is touched.

**"Start Building next 20 items" - Cashier no longer sees PIM Reports, labelled fields and darker muted text, one number and date format everywhere, 2,764 inline styles moved to classes, faster screen switches and inbox, a batch-loading synthetic client, a release-candidate check at Deploy, threshold-splitting guard, joiner / mover / leaver approval, sign-in addresses, security-log history backfill, setup checks from the Finance pack** *(code + tests + guides, session erp-0c; released in 0.23.0 `1c026d1`, not yet deployed)*

What changes for existing clients: a Cashier no longer sees PIM Reports; numbers and dates read the same on every screen (day-first, `03 Feb 2026`); saving an Employee no longer re-activates a disabled user and a leaving Employee disables their user; Users shows each user's last sign-in address; the Security tab gains Access review and, on the Event export box, History from / Send history; Deploy stops before the backup when the commit has no passing release-candidate record. Two new settings, both **Off**: `approvals.threshold_split_guard` and `security.identity_change_approval`.

**"Once both done, Push and release ... DO all 4 of above" - the four steps left by "Implement all": the disk clean-up run, a duplicate price-override function removed, recommendations for the rest of the unused code, the old remote branches, then the release** *(session erp-30, was erp-0f)*

Detail is in the tracker under *OPS › Repository & developer tooling* (103.1, 103.4.1, 103.5).

## 2026-11-11

**"Start next 20 open items building" - the ledger in the caller's transaction, 18 more keyed routes, locked-record answers, stated import totals, the reason-code catalogue, faster screen switches, long lists that filter every row, full-disk safety, Go 1.27.4 with govulncheck blocking, the upgrade channel, history / binary / backup secret scans, a bigger synthetic client** *(code + tests + guides, session erp-8c; released in 0.23.0 `1c026d1`, not yet deployed)*

What changes for existing clients: reversing a journal voucher is one step and a second reversal is refused; editing a dispatched challan or a posted purchase return says what to do instead of a bare refusal; an opening-balance import can state its totals and approval refuses a file that does not tie; reason codes gain **Also used in**, **Severity** and **Needs a note**, plus 23 starter codes, and with codes required a transfer shortfall or a sales return needs a code; a second sign-off on a cutover pack is refused; switching back to an open screen no longer redraws its star and views twice; a filter or sort on a long list includes every row; a full disk leaves no partial audit archive or logo; new clients get every starter reason code; backups are written owner-only with the key kept off the command line (the restore drill too); Deploy honours the host's `UPGRADE_CHANNEL`.

**"All approved" - the twelve ask-162 decisions taken as recommended; seven built: store-only material issue, a reason to close a period that is not ready, a role-aware sidebar, server-side sign-out and idle limit, rehearsal on for new clients, a release-evidence schedule, a footprint entry per release** *(code + tests + guides, session erp-e7; shipped in release 0.23.0, `1c026d1`)*

What changes for existing clients: a Cashier no longer sees **Issue** / **Return unused** on Material Issue (Store Manager and HR/Admin do); closing a period with a failing check asks for a reason; the sidebar hides Trial balance, Chart of accounts, WMS labour and four PIM pages from roles that cannot read them; **Sign Out** now ends the session on the server. Unchanged unless a client chooses: the server idle limit is 0 (off); `platform.require_rehearsal` stays Off for existing clients (new clients start On).

**"Implement all" - repository restructure, phase two: migrations in `db/migrations/`, about 480 files renamed, nine unused functions removed, a disk clean-up script for the owner** *(code layout + docs, session erp-0f, run header erp-28; NOT COMMITTED)*

The owner's answer to the Stage 103 decisions and the renames §338 left until the other sessions were idle. Detail is in the tracker under *OPS › Repository & developer tooling*. The owner's explanation is section 6 of `docs/engineering/repository-restructure-2026-11-10.md`, and every old and new path is in `docs/archive/renamed-files-2026-11-11.md`.

**"Build all recomended" - the reel review's three decisions built: Deploy alerts warning, browser errors in the System Log, a browser smoke run as a release gate** *(code + scripts + guides, session erp-10; NOT COMMITTED)*

The user approved the recommended answers to §340's open decisions.

**"Checklist is still organisation required" / "Stage 35 onwards it is not organised" - the stage write-ups filed by module in the archive** *(docs only)*

After §337 the live tracker still ended with 67 `### Stage N` write-ups under Background notes, and the archive held 115 more in no order. A one-off script, not kept in the repo, moved them all into the archive's Background notes:

## 2026-11-10

**"WAtch all insta video and derview the insight and prepare which is required and which is not with reason" - six AI-coding reels judged against the ERP** *(review only, no code)*

Six Instagram reels (CI and tests; SQL injection; exposed keys, open database and an id changed in the URL; the ponytail plugin; one agent with a playbook per job; "vibe coder vs AI engineer") were downloaded, transcribed with a local Whisper model and read frame by frame in scratch, then each claim was checked against the tree. Most is already in place: CI and the full suite on a fresh DB, parameterized SQL (the document filter key passes `safeFilterKeyRe` and the doctype's declared fields), no secrets in `public/`, tenant and insider isolation tests, human-gated destructive steps, skills and scripts as playbooks. Not adopted: ponytail (repeats Article 1 and loads every session), docs via MCP, per-PR agent review and auto fix-PRs, "strip semicolons" sanitizing. Four gaps filed as decisions with a recommendation each: 158.2 production alert webhook never checked, 158.3 browser errors never reported, 158.4 no clicked smoke check in the release, 158.5 UI rules only in Claude's memory (since answered by Article 7, item 159.1). Not committed. Detail: items 158.1-158.5.

**"Arrange in proper std and structured way... one core rule like a constitution" - CONTRIBUTING.md, the layout guard, the root tidy** *(docs + one test + scripts, session erp-3f; NOT COMMITTED)*

Items 159.1-159.3, 103.2, the root part of 103.1, `scripts/archive/` from 103.4, and 158.5 (answered) in `micro_checklist.md` under *OPS › Repository & developer tooling*. The full explanation the user asked for, with every move, deletion, rule and what waits, is `docs/engineering/repository-restructure-2026-11-10.md`.

**"Checklist rebuild task" - the tracker rearranged by ERP module, skeleton permanent** *(docs + one script)*

The user found 120+ stage sections too many to read. `docs/micro_checklist.md` and its archive are now arranged the same way: 24 permanent modules (PIM, PROC, INV, WMS, OMS, POS, CRM, FIN, TAX, HR, MFG, ASQ, RPT, PRT, CHN, DATA, CFG, TEN, SEC, PLAT, UI, OPS, QA, DOC) and 130 sub-modules, with each module's open and built counts in a Module map at the top. All 1,898 top-level items (491 live, 1,407 archived) were sorted into sub-modules by five classifier passes against one taxonomy, then spot-checked. Ids, text and order within a sub-module are unchanged, so code comments and the release commit message still find them. Stage write-ups, minus their items, sit under Background notes in each file, so old stage anchors still resolve. New asks get a run header at the end of the Asks log and items in their sub-module; no new stage sections. `docs/update-checklist-map.ps1` refreshes the counts and *Nothing open.* markers, prints the next free ask number, and fails on a skeleton that differs between the two files, an open item in the archive, or an item outside a module. The release runs it in G7. Its commit-title fallback now reads run headers as well as `## Stage` lines. A session filed Stage 158 the old way mid-rebuild; its five items were moved into modules the same day. Verified: open and built line counts, every item line and every prose line preserved; no broken links; doclint clean; brain claims the new script. CLAUDE.md's documentation conventions rewritten. Not committed. Detail: the "Checklist rebuild task" run header in the tracker's Asks log.

**"WHich are built from checklist archive it" - built work moved out of micro_checklist** *(docs only)*

Same split as §174, by a checkbox-and-status script: 35 closed stages moved whole, and from stages still open 10 closed sub-groups and 475 `[x]` items, into `docs/archive/micro_checklist_closed_stages.md` (724 `[x]` lines). A block moved only with no `[ ]` and no unfinished status or open decision; 12 stages flagged only by stale notes were checked by hand and moved. Stages 49, 146, 148, 155 left alone (other sessions' uncommitted edits). Relative links rewritten for the archive folder, all resolve; open-line count unchanged. Tracker 1,571 KB → 920 KB. Not committed. Detail: the 2026-11-10 entry at the top of micro_checklist.

## 2026-11-05

**"Build next 20 items" - Stage 148 Quality and optional modules, seven open bugs, the rest of Stage 146 with no blocker** *(code + tests + guides, session erp-22; NOT COMMITTED)*

What changes for existing clients with no setting: an OMS bill now carries the order's shipping charge with its GST; a returned free gift credits the gift account the sale charged, not cost of goods sold; Ready to pay leaves out bills still waiting for their PO, which sit on a Waiting for PO tab; a PO routing rule must name roles that exist; download file names are cleaned; a product sheet saves whole or not at all. Everything else here stays off until a module or setting is turned on.

**"Start building next 20 items" - Stage 148 Finance, Selling, Memo stock and Vendor link, five half-built Stage 49 items** *(code + tests + guides, session erp-4e; NOT COMMITTED)*

The ask, the 20 items and the result are on the "Start building next 20 items" (erp-4e) header in `micro_checklist.md`; each item's line carries what was built, how it was verified and what is left. This section is the summary and what the run taught.

## 2026-11-04

**"I told to take next piece of point not already being build 20 point to build. Once this is fixed, Take next 20 to build" - Stage 140.14 screen fixes, what was left of 148.1 / .2 / .3 / .4 / .6, buying W2 and W3** *(code + tests + guides, session erp-60; NOT COMMITTED)*

The ask, the 20 items and the result are on the "Next 20" header in micro_checklist.md; each item's line carries what was built under **erp-60 (2026-11-04, second run)**.

**"start next 20 item ro build" - Stage 148 W1 and W2, the PIM phase of Stage 146, half-built 49 parts** *(code + tests + guides, session erp-60; NOT COMMITTED)*

The user's ask, the items and the result are on the erp-60 claim header in micro_checklist.md; each item's line carries what was built under **erp-60 (2026-11-04)**. Peers were asked first; erp-48 keeps Stage 143. The user then added "take next piece of point not already being build" and "Once this is fixed, Take next 20", so the next run's header sits below this one.

**"Once deployed, Start building next20 items" - the rest of Stage 146 plus half-built 47 / 49 / 157 parts** *(code + tests + guides, session erp-3f; in release 0.23.0 `cbf2799`)*

The user's ask, the 20 items and the result are on the erp-3f claim header in micro_checklist.md; each item's line carries what was built under **erp-3f (2026-11-04)**. erp-79, erp-d8, erp-34, erp-48 and erp-f8 were asked first and claimed nothing.

**"Push and deploy. All approved" - 0.22.0, every session's work, deployed to app and minn** *(release, session erp-d8)*

User, 2026-11-04: *"Push and deploy. All approved"*, then *"Deploy"*.

## 2026-11-03

**Stage 157 — The UI fits any window size or browser zoom** *(CSS + guide, session erp-79; NOT COMMITTED)*

The user's POS screenshot showed the top bar's second row (store, Refresh, Reset, ?) drawn over the page in a narrow window, and they asked for a UI that adapts at any zoom. Items 157.1-157.4 in micro_checklist.md carry the detail.

**Stage 146 - 20 of the vendor-recording gaps built** *(code + tests + guides, session erp-e9; NOT COMMITTED)*

The user's ask (the next 20 items, half-built first, skip items in flight) and the result are on the erp-e9 claim header in micro_checklist.md; each item's line carries what was built under **erp-e9 (2026-11-03)**.

**"Build next 20 items" - 20 half-built Stage 47 / 49 items taken further** *(code + tests + guides, session erp-e8; NOT COMMITTED)*

The user's ask, the rule (half-built items first, skip items in flight, 20 in all) and the result are on the erp-e8 claim header in micro_checklist.md; each item's line carries what was built under **erp-e8 (2026-11-03)**.

## 2026-11-01

**Stage 153 — Sticker Printing ▸ Print from Transaction: select all** *(code; NOT COMMITTED)*

The user's ask ("need a select all in print", on a Print from Transaction screenshot) is quoted on the Stage 153 header in micro_checklist.md.

## 2026-10-31

**Stage 149 — No explanatory blurbs anywhere; PIM Workbench loads on a full catalog** *(code + tests, session fb7b6adf; 149.2 and part of 149.1 shipped in 0.21.0, the rest NOT COMMITTED)*

The user's asks (the Bulk CSV Import screenshot, then "PIM workbench is not loading") are quoted on the Stage 149 header in micro_checklist.md.

**Stage 150 — Switching the store keeps every screen where it is, like an app** *(code + docs, session erp-f2; NOT COMMITTED)*

The user's ask (an RFQ ▸ New RFQ screenshot) is quoted on the Stage 150 header in micro_checklist.md.

**Stage 148 — What a 138-episode D365 F&O course shows the ERP should do, smarter** *(research + docs, session erp-f2; plan shipped in 0.21.0)*

The user's ask is quoted on the Stage 148 header in micro_checklist.md.

**Stage 146 — What 12 vendor meeting recordings show that the ERP lacks: blueprints, gaps, plan** *(research + docs, session erp-99; NOT COMMITTED)*

The user's ask is quoted on the Stage 146 header in micro_checklist.md.

**Stage 151 — HRM: each HR page its own sidebar entry, like PIM** *(code + docs, session erp-hr1; shipped in 0.21.0)*

The user's ask (an HRM ▸ HR ▸ Appraisals screenshot with the tab strip) is quoted on the Stage 151 header in micro_checklist.md.

**"Release all changes until now from all work" - 0.21.0, every session's work, deployed to app and minn** *(release, session erp-b6)*

User, 2026-10-31: *"You can ask everyone to stop working unill you say. & Release all changes untill now from all work"* (after *"Relase now. That is high priority to test"* for the barcode print, 143.6).

**Stage 147 — Item boxes find by Design ID, Combination ID or barcode; the OMS order line shows stock and fills the price** *(code + tests + docs, session erp-oms; released in 0.21.0 `4b80806`, deployed to app and minn)*

The user's ask (an OMS ▸ New order screenshot, then two corrections: Design ID, Combination ID / SKU and barcode may all carry letters) is quoted on the Stage 147 header in micro_checklist.md.

**Stage 145 — Inventory: a one-page management dashboard, every inventory page on the top-bar store, Stock on Hand by design with columns each user picks** *(code + tests + docs, session erp-inv; shipped in 0.21.0)*

The user's ask is quoted in full on the Stage 145 header in micro_checklist.md; items 145.1-145.4 carry the detail.

## 2026-10-30

**"Push and deploy properly ... all approved" - 0.20.0, everything in the tree, deployed to app and minn** *(release, session erp-1f)*

micro_checklist run header "Push and deploy properly" (2026-10-30).

**"Build next 20 open items" (session erp-6b): twenty Stage 47 / 49 slices - a cross-store write hole, raw database text in errors, a screen burst refused, a posted GRN cancellable, strict posting transitions, revocable exports, job actors** *(code + tests + docs; NOT COMMITTED)*

The user's "build next 20 open items" (2026-10-29). erp-7c held the six approved decisions and 140.12 was erp-91's; the Stage 47 / 49 items no run had touched needed a reviewer, an environment, counsel or real users. So the run took the "Left:" slices earlier runs had named and two not-started follow-ups: 47.10.7, 47.11.2, 47.11.3, 47.11.8b, 47.13.3, 47.14.3, 47.17.5, 47.18.3, 47.18.4, 49.3.8, 49.4.6, 49.4.7, 49.5.1, 49.5.3, 49.5.4, 49.8.2, 49.8.5, 49.8.6, 49.10.4, 49.11.1. Item detail: each item's entry in micro_checklist and the run header after the erp-7c block. Seven are done and ticked; thirteen are built in part, with what is left on each.

## 2026-10-29

**The erp-d3 follow-ups: offline sales on the till's day, held offline cash in the drawer, SKU scans, no PO for a stateless store** *(code + tests + docs; released 0.19.0 932f3c8)*

The user's *"all recommended"* on the six follow-up decisions of §304 (micro_checklist 49.5.6d, 49.5.6e, 47.11.8b).

**Stage 141 — Wholeops beside a client's brand; refreshes that keep the brand and the menu; a rate limit sized for real use** *(code)*

From the user's minn screenshots (branding on: Wholeops only as grey foot text), their pick of four real-screenshot mockups (*"c1 b2"*), and a video of repeated refreshes on app.wholeops.in. Full detail in checklist Stage 141.

**"Build next 20 items" (session erp-5e): twenty Stage 47 / 49 slices - privilege-change evidence, two hostile clients, insider scenarios, GST / Companies Act controls, receivables vs ledger, synthetic tenants, slow-query shapes, CERT-In logs, a secret scan** *(code + tests + docs; released 0.19.0 `932f3c8`)*

The user's "Build next 20 items except 140's list" (2026-10-28). Everything open outside Stages 47 / 49 waited on the user, a device, a credential, a decision or another session, so the run took the twenty Stage 47 / 49 items no run had touched that have a self-contained slice: 47.9.1, 47.9.3, 47.12.4, 47.13.2, 47.13.5, 47.14.3, 47.14.4, 47.16.1, 47.16.3, 47.16.4, 47.18.2, 49.3.6, 49.3.7, 49.5.5, 49.5.7, 49.6.2, 49.6.6, 49.8.8, 49.10.3, 49.10.6. Item detail: each item's entry in micro_checklist, and the run header under erp-d3's decisions block. All twenty are built in part; what is left is on each.

## 2026-10-28

**The erp-d3 decisions: offline sales validated before they post, vendor state required, unknown client codes refused, nothing booked twice, per-client date order, outside calls switch** *(code + tests + docs; released in 0.19.0 `efc6403`)*

The user answered the eight decisions the "next 20" run (§300) left open; item detail: micro_checklist block "User, 2026-10-28: answers to the erp-d3 decisions" (49.5.6a-e, 49.3.2a, 49.2.1a, 47.11.8a-b, 47.14.2a, 49.8.7a, 49.10.8a, 49.3.4b).

**Stage 135.21-135.26 — every posting under its document's lock; provisioning seeds stock locations; PIM publishes to Shopify linked** *(code + tests + docs, released in 0.19.0 `efc6403`)*

The user: *"Yes, Acknowledged all as recommened"* - build 135.21 and 135.23, route Shopify channels in PIM Publish through the Shopify page (135.22), and the 135.10 / 135.18 answers confirmed. Item detail: micro_checklist 135.21-135.26.

**Stage 140 — Guide runs the whole organisation cycle on app as user, BA, audit, accounts and customer** *(live run + docs; no ERP code changed)*

The user asked Guide to go through every module as each kind of user, on app.wholeops.in, and to end with two documents: the process, and the enhancements filed in the tracker module by module. Guide got a new suite, `orgcycle`, for the cycles its existing suites did not cover: a customer's sales, advances, credit invoices, receipts, credit note and return; a website order through Accept, rack pick, label, handover and an OMS return; Shopify, Amazon and Myntra templates; CRM segment and campaign; and a before-and-after reconciliation of every report that ties to the GL. Each cycle was rehearsed on a local copy of 0.18.0 until only real findings were left. The user then approved the listed writes, and Guide ran `orgcycle`, HR and the procurement / quality / service flows on the test store, and finally all 421 reports.

## 2026-10-27

**The approved follow-ups: one statement per request, a lighter font, eight list columns; 0.18.0 released and deployed** *(code + tests + docs, NOT COMMITTED)*

The user approved the §296 batch and its five open decisions. The batch shipped in 0.18.0 (`9a008cf`), with the parallel sessions' finished work, and was deployed to app and minn after a verified backup. Before the deploy, the data-change scan learned a check it had missed: a background sweep that starts deleting a table it never touched (the retention sweep had gained three) now counts as a data change. Release G9 and Deploy D1 both list it, and the deploy waits for YES. The user answered YES.

**"Build next 20 items": fourteen Stage 47 / 49 slices** *(code + tests + docs, NOT COMMITTED - held in release/hold.txt)*

The user: *"Build next 20 items"*, then *"go ahead"*. Stages 47 / 49 / 50 had no unblocked item left in tracker order, so the run took the six not-started Stage 135 findings and the fourteen Stage 47 / 49 items earlier runs had passed over. Item detail: micro_checklist header of 2026-10-27 and each item's entry.

**Stage 135.10-135.22 — decisions, stock integrity and locking** *(code + tests + docs, released in 0.19.0 `efc6403`)*

The user: *"Build all. All recommended one is approved"* - the open Stage 135 items, with the recommended answers to 135.10 and 135.18. Item detail: micro_checklist 135.10-135.22.

**Stage 128.5.1 — minn and app configured from their Setup Workbooks** *(live data through Guide + one engine fix, fix released and deployed in 0.18.0 `9a008cf`)*

The user asked for minn's complete setup, then app's, with a record of what was done in an .md file (`Downloads\MansoorBusiness\client-setup-minn-and-app.md`). Item detail: micro_checklist 128.5.1.

## 2026-10-26

**Stage 139 — ERP shortcuts: one Ctrl+K box, short codes, report numbers, standard keys** *(code + migration + docs, NOT COMMITTED - held in release/hold.txt)*

The user asked for ERP shortcuts done the easy, best-practice way, a number for every report, and what else would make transactions easier. The plan (`docs/product/shortcuts-plan-2026-10-26.md`) was approved with "Build all of the above", with the shortcuts written up properly in the user help. Item detail: micro_checklist Stage 139.

**"Build next 20 open items": Stage 49 finished, Stage 50's buildable packages** *(code + tests + docs, released in 0.18.0 `9a008cf` and deployed 2026-10-27)*

The user asked for the next 20 open items. They were taken in tracker order after 49.17.4: the eleven left in Stage 49, then Stage 50's work packages that need no decision, host or real users (BLD-002, 028, 029, 032, 041, 044, 045, 053, 055). Four are fully done (49.18.3-49.18.6); the rest are built, with what is left written on each. Two packages (BLD-041 print pass, BLD-045 load time) ran as background agents. Release 0.17.0 (session erp-18) shipped while this was in progress, with this batch held.

**"Build all" run, Stage 49: the next 20 items** *(code + tests + runbooks, released in 0.15.0 `21e7c99`)*

The user asked for the next 20 open items, to start once release 0.14.0 (`0bcac93`) was pushed. They were taken in tracker order from 49.11.2, leaving out the items that need legal advice, live drills, a decision or real users. Five are fully done (49.11.3, 49.11.8, 49.12.2, 49.12.6, 49.12.7); fifteen are built in part, with what is left written on each entry.

**Stage 138 — No "Switch product" box for a client** *(frontend + `/me/modules`, released in 0.14.0 `0bcac93`)*

The user asked why minn's sidebar showed "Switch product..." and wanted a client to see app's clean layout with only the modules it lacks hidden. Stage 27's switcher appeared for a client with 2+ products but not every full-suite module, and picking one only changed the address bar. Removed the switcher, the server's `owned_packages` and its two helpers; the sidebar's existing module hiding is unchanged. Detail in `micro_checklist.md` 138.1; minn check after the deploy is open.

## 2026-10-25

**"Build all" run, Stages 47 / 49: the next 20 items** *(code + tests, NOT COMMITTED)*

The user asked for the next 20 items. They were taken in tracker order from Stages 47 and 49, each built as a self-contained slice with its own test. The twelve items Stage 134 had already closed were ticked first. Five items are fully done. Fifteen are built in part, with what is left written on each entry.

**Stage 137 — Printer presets and one PrintBridge for every computer** *(code + PrintBridge 0.2.0, NOT COMMITTED)*

The user asked whether printing was ready: stickers came out misaligned, they wanted a preset set once in config that stops the asking, and PrintBridge had to work on any computer, wake when the ERP is used, and come back after a Quit. Decisions: stickers; the printer's roll wins over item template rules; one installer from the ERP; Windows. Built: a Printer preset (roll, shift, darkness, speed) applied on the server (`applyPrinterRoll`) and in the sticker engine, with Test alignment and Calibrate roll; PrintBridge 0.2.0 trusts every `*.wholeops.in` address, asks once about any other, registers a `printbridge:` link that the ERP opens to wake it; the installer is served from `public/downloads` and a release gate keeps its version current. Verified on a fresh scratch DB, the agent's tests and Chromium with a recording stub; the installer and the paper alignment wait on the user's PC. Detail: micro_checklist Stage 137.

## 2026-10-24

**Stage 90.6 — Releases in a few words** *(migration + deploy script, NOT COMMITTED)*

The user found 0.13.0's automatic note too long and the older rows unchanged. The automatic note is now one line (each title's headline, first three, "+N more"); `db/migrations_stage90_6_release_notes.sql` rewrites 0.1.0-0.13.0 to a few words each at the next deploy, so no one runs anything by hand (Claude is blocked from production writes). Notes cell smaller and muted. Detail: micro_checklist 90.6.

**"Build all" run, first slice** *(session erp-f9; paused for a release)*

The user asked to build everything open that needs no decision. The run was recorded at the top of the checklist and stopped after its first four items when the user asked to release what was done. **132.10** fixed the eight bugs Guide found. Two error codes reached users as "Unexpected error occurred"; a new test now fails on any engine or handler code without a catalog row. Soft-deleted masters still counted as Active, which blocked new setup and let deleted credentials, offers and campaigns keep working; 82 queries now exclude them, and restoring a record re-runs its rules. Other fixes: a wrong "barcode" headline on every other duplicate (new MASTER-1321), a crash on an empty integration log, operator-only screens in a client's menu, and New offered on record types the generic save refuses. **135.2-135.4** brought thirteen KB handbooks in line with the screens that exist now (one subagent swept nine of them), put a dated-baseline note on the 2026-10-04 plan, and gave the capability register a label with no version number. **135.3** corrected the publish worker's "stub" comment. The sweep found a real bug (Book Pickup always fails) and two doc gaps, recorded as 135.11-135.13. Details: checklist 132.10, 135.2-135.4.

## 2026-10-23

**Stage 90.5 — The Control Panel's Releases table says what changed** *(deploy script + Control Panel, NOT COMMITTED)*

From the user's Releases screenshot: the note said only "Release.cmd 1356183 from 0.11.0 (d5d7668)". `deploy/deploy.ps1` now builds the note from the commits since the live build (`Get-ReleaseSummary`: Release.cmd's "- " title lines, hand feature/fix subjects, stage numbers dropped, one line each) and sends it base64-encoded, because the old sanitiser stripped every comma, colon and dash; `remote_deploy.sh` decodes it; `Invoke-Deploy.ps1` no longer passes its own note. The column is **What changed**, one line per change. Verified against all ten past release ranges; first live row at the next deploy. Detail: micro_checklist 90.5.

**Stage 134 — The five open decisions taken and built: security groups, sticker bugs, client branding, Railway staging** *(code + docs, NOT COMMITTED)*

**Stage 136 — Transaction tables audit** *(audit + one-line fix)*

The user asked whether every transaction's table is built and working, whether the pending checklist missed a table, and which upcoming items need one. Every migration in the working tree was applied to an empty scratch database, and the result was compared with every table and record type the Go code and screens use. Nothing is missing. The local client schemas match `tenant_default`, and both halves of the open checklist were swept by two subagents. The full suite on copies of that database turned up one real miss: Stage 134.3's `inbound_webhook_receipts` was never classified for the Stage 91 data reset (fixed, 136.2). The storage that the 73.4.x and other open items will need is listed in 136.1. A read-only production check was handed to the user, because the classifier keeps production reads with the user. Detail: **micro_checklist.md Stage 136**.

