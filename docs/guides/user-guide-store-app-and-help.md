---
doc_id: DOC-78F8FF850A-09
title: User Guide: Store app, profile and help
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# User Guide: Store app, profile and help

Part 9 of 9 of the [User Guide](user-guide.md). Previous: [Finance, service, CRM, people and approvals](user-guide-finance-crm-and-approvals.md).

## 10A. The Store app on your phone

The **Store app** is a small app for the shop floor: decide approvals; scan any item to see its price, MRP, stock at your store and your other stores, and how old a batch is; see today's sales (managers); check deliveries and transfers, count stock and ask for labels by scanning; build a basket for the till; and order a size from another store for home delivery. It is part of the ERP - your company's ERP address followed by **/store/** (for example `https://yourcompany.wholeops.in/store/`) - and you sign in with your usual username and password, or with a PIN on the store's shared phone.

### 10A.1 Install it (once per phone)

1. Open the address in **Chrome** on an Android phone, or **Safari** on an iPhone.
2. Sign in (§10A.2).
3. **Android (Chrome):** the **⋮** menu → **Install app** (or **Add to Home screen**). **iPhone (Safari):** **Share** → **Add to Home Screen**.
4. A **Store** icon appears on the home screen and opens full screen, like any app. There is no app store: updates arrive by themselves the next time you open it.

### 10A.2 Sign in

- Your username and password, as in the ERP. If your role uses two-factor sign-in, enter the code from your authenticator app, or tap **Use a recovery code**.
- Setting up two-factor sign-in for the first time, or replacing a password your administrator set: do it once in the full ERP (the app says so), then sign in here.
- Signing in here also signs you in to the ERP in the same browser, and the other way round. **Sign out** on the app's home screen signs you out of both.
- The app signs you out after the same time without use as your **Auto Logout** setting (§11).
- **On the store's shared phone** (one a manager registered to the store, §10A.14) the app opens on **Sign in with your PIN**: type your username (it is offered once you have signed in on that phone) and your PIN. You set your PIN yourself in **Settings** (§10A.14) after signing in once with your password. A PIN session works at that phone's store only, locks after a few minutes without use (5 unless your administrator changed it) so the next person signs in as themselves, and lasts at most one shift (12 hours). After 5 wrong PINs, PIN sign-in stops for you for 15 minutes; your password still works (**Sign in with password instead**). Administrators always sign in with the password and code.

### 10A.3 Your store

The button at the top right shows the store you are working at. If you may work at more than one store, tap it and pick one; the ERP in the same browser switches with you. If the button cannot be tapped, you work at one store only.

### 10A.4 Approvals

**Approvals** lists only what is waiting for **you** - the same approvals as **My work** (§10). The number on the home screen's tile is how many.

- **Show details** shows the document's main fields.
- **Approve**: tap it, then tap **Tap again to approve** within 4 seconds. The second tap is there so a slip of the thumb never approves anything.
- **Reject**: tap it, type why (required - the person who sent it sees your reason), then tap **Confirm reject**.
- If a decision gives you something to note down (for example a one-time password for a password-reset request), it stays on screen until you tap **Done**.

### 10A.5 Scan an item

Tap **Scan an item**, then do one of these:

- **Scan with camera** (Android Chrome) and point the camera at the barcode;
- with a **Bluetooth scanner**, tap the box once, then scan - it looks the code up by itself, whether or not the scanner sends Enter;
- or type the code and tap **Look up**.

It accepts an item code, its barcode, a receipt-date barcode, or a **GRN batch number** from the sticker (capital or small letters). You see:

- the picture, name, code and barcode;
- the **price** the till charges a walk-in customer (the default price list, else the item's sale price, else its MRP), and the **MRP**, struck through when the price is lower. Offers and a customer's own price list are applied at the till;
- **Stock**: your store first, then every other store you may work at that holds some. *Available* is what can be sold now; *on hand* also shows when some of it is reserved or on hold;
- for a scanned **batch**: the day it came in, how many days ago, the GRN and the supplier;
- the **oldest batches with stock left**, oldest first, so old stock can be sold first. What is left of each batch is an estimate across all stores, the same as the **Batch Lookup & Ageing** report (§6.3).

Cost is never shown in the app.

**On an iPhone** the browser cannot read barcodes with the camera yet - use a Bluetooth scanner, or type the code.

### 10A.6 No connection

The app still opens and says **You are offline**; when the connection returns it carries on by itself. A **count** works offline (§10A.9); everything else needs a connection.

Under the item's details, **Scan an item** also offers what you can do with it: **Add to basket** (§10A.11), **Print labels** (§10A.10), **Ask for stock** (§10A.9) and, when your store has none free, **Order for home delivery** (§10A.12).

### 10A.7 Today (store managers)

**Today** shows your store's sales so far today: **Sales**, **Bills**, **Average bill**, **Items per bill** and **Returns**, each beside the same day last week up to the same time (there is no sales target in the ERP yet, so last week is the yardstick - the screen says so). Below: the day's **top items** and the **salesmen** by sales. If you run more than one store, **All my stores** adds them up. Only store managers and administrators see Today.

### 10A.8 Receive by scanning

**Receive** lists what is waiting at your store: **goods receipts** at their check (Verify) step and **transfers** on their way here. Open one and scan each piece as you unpack (or tap **−** / **+**, or type the number). Each line turns green when it matches, amber when short, red when extra; a code that is not on the delivery is listed separately and is not saved. Your counting is kept on the phone until you save, even if you leave the screen.

- **Goods receipt:** **Save the check** records what you counted as the receipt's quantity - the same as Verify on the computer (§6.10) - and the receipt moves on to the price check. More than the PO asks for: choose **Keep it** (needs approval) or **Send it back**. Only someone allowed to verify receipts can save; anyone can count.
- **Transfer:** **Receive into stock** puts what you counted into this store's stock. If some is short, choose **More is coming** (the rest stays in transit) or **Nothing more is coming** (the rest is closed as lost); a shortage you close or an extra piece needs a **reason**. A transfer with serial-numbered items, or with more than one lot of an item, is received on the computer (Stock Movement).

### 10A.9 Count by scanning, and ask for stock

**Count** → **Start a spot count**, then scan every piece on a shelf or in a bin (one scan per piece; fix a number with **−** / **+**). **Finish and send** records it as a cycle count: a count that matches the stock is posted at once; one that differs goes to the store manager for approval before stock changes, as with **WMS → Cycle Count** on the computer. With **no connection**, the count stays on the phone and is sent by itself when you are back online - the home screen says how many are waiting. Sending the same count twice changes nothing.

A **full count** (physical inventory) is started by the warehouse team; while it is open it appears under **Full counts at this store**. Several people can count the same full count on their own phones: each phone's numbers are added together, and a phone that sends again only replaces its own numbers. An item the count did not list gets a line of its own. The warehouse team reconciles and closes it.

**Ask for stock** (from an item): pick where it comes from (a warehouse or another store, with what each has free) and how many. This makes a transfer request and sends it for approval, the same as **Stock Movement** on the computer.

### 10A.10 Print labels from the phone

A phone cannot reach the label printer itself, so it asks the store's computer. From an item: **Print labels**, the number of labels, an optional note → **Send to the store's printer**. On the computer, **Sticker Printing → + Print Stickers → Print from Transaction → Store Label Request**, pick the request, and print as usual - the computer's own printer choice applies. **Labels** on the phone lists the store's requests: **Waiting** until the computer prints it, then **Printed** with the time.

### 10A.11 Basket for the till

Help a customer on the floor, then send them to the till with their basket. **Basket for the till**: scan what they want (**−** / **+** for quantity), add their name or phone if you like, **Send to the till**. The phone shows the basket's number and a barcode. At the till: **More... → Resume a held bill**, then pick it from the list or type or scan its number in **Find a basket**. The till prices it again when it is resumed, so offers and the customer's own prices apply.

### 10A.12 Order for home delivery

When your store does not have a size, scan it and tap **Order for home delivery**: how many, where it ships from (a store you can see that has it, or **Let the order desk choose**), the customer's name, phone and address with its PIN code, and whether they **pay on delivery** or have **already paid**. **Book the order** makes an ordinary order on the order desk (OMS → Pick, Pack, Ship), already accepted. If no store has all of it, the order is made **on hold** and the order desk sources it. Tapping Book twice never makes two orders.

### 10A.13 Alerts

**Settings → Alerts on this phone → Turn on alerts** (allow notifications when the phone asks). The phone then shows an alert when **an approval is waiting for you** or **an item runs low at your store**, even when the app is closed; tap it to open the right screen. The first alert comes for something new - what was already waiting when you turned alerts on is not repeated. Untick what you do not want and tap **Save**, or **Turn off**. On an iPhone, add the app to the Home Screen first and open it from there; alerts work on iOS 16.4 and later. **Low stock** (home screen) lists the store's low items at any time.

### 10A.14 Settings: your PIN and this phone

- **Your PIN:** type your password (to confirm it is you) and a PIN of 4 to 8 digits - not all one digit or a run like 1234 - then **Set PIN**. **Change PIN** and **Remove PIN** are here too.
- **This phone (store managers):** on a phone the whole team shares, **Register to <store>** with a name for it (e.g. *Counter phone 1*). From then on everyone signs in on it with their own PIN. **Store phones** lists your stores' registered phones; **Remove** one that is lost, stolen or retired and everyone signed in on it is signed out at once.

### 10A.15 Serve a customer

**Serve a customer**: type their phone or name and tap **Find**, then open them. You see their lifecycle, whether they take offers on WhatsApp, sizes, points (and any pending), spend and last bill, their last bills, wish list and notes.

- **Wish list**: scan or type an item and **Add**. **Ask the store to keep it** sends a hold request (§9D.11).
- **Send a look on WhatsApp**: scan the item, then **Send on WhatsApp**. Your own text reaches them only if they wrote to the store on WhatsApp in the last 24 hours; otherwise choose an approved offer template under **Send as** (the item is filled in). If it cannot go, the screen says why (for example they have not agreed to offers, or stopped all messages).
- **Notes**: type and **Save the note**; the team sees it on the customer page.
- **Has <name> agreed to get offers on WhatsApp?** Tap **Yes - they agreed just now** only when they have said so; it is recorded with your name and the time.

### 10A.16 My tasks

**My tasks** lists the CRM tasks for you, and those for anyone at your store (arrivals, alterations, custom orders, follow-up calls), with the customer and when each is due (*overdue* in bold). **Customer** opens the customer; **Take it** makes an anyone-at-the-store task yours; **Done** asks *How did it go?*, then **Save - done**.

### 10A.17 Inspect

When lots are held for inspection at your store, an **Inspect** tile appears on the home screen with the number waiting. Open it: each lot shows the item, the quantity, why it was held and the sample size. Every result line starts as **Fail** with the full quantity. Change the passing line to **Pass**, or split the quantity over more lines with **Add line** (the lines must total the lot). Fill the reading for each test, and **Karat** and **Weight** if asked, then **Save**. A lot with failed units left shows below, with **Scrap** (choose a **Scrap reason**; the list is your Inventory Adjustment reason codes) and **Release** (needs a note). **Waive** shows only where the item allows the warehouse to decide, and needs a reason. You see only your own store's lots.

## 11. Your Profile, Password, and Session

Click your name at the bottom of the sidebar to open your account menu, then **My Profile**. From there you can see your role and (if set up) your linked employee record, change your own password, and set how long the system should wait before automatically signing you out if you step away — separate from the account-wide session limit your admin controls.

**Auto Logout (inactivity)** is under **Contact & Session**: Never, 15 minutes, 30 minutes, 1 hour, 2 hours or **12 hours**. It starts at **12 hours**, which covers a full working day, and stays there unless you change it. Pick a value and click **Save Changes**. Working in any tab of the ERP counts as activity, so a second tab left open in the background will not sign you out while you work in another one.

**Sign Out** ends your session on the server too, so the page cannot be reopened with the old sign-in. Your other sessions (another browser, your phone) stay signed in. Your administrator may also set a **server idle limit**: after that long with no use, the next click asks you to sign in again.

### 11.1 Which version you are on

At the **bottom of the sidebar, in the right-hand corner of your name box** (beside your role, under the small arrow), there is a small grey number:

```
0.1.0
```

That is the release of the system you are using. **Hover over it (or tap it on a phone)** and a small popup shows the same number with the date that release came out:

```
0.1.0
4/10/26
```

The date is day/month/year — so `4/10/26` is 4 October 2026.

**Why it matters:** if you ever report a problem, quote both lines. They tell whoever helps you exactly which release you were on, which is often the difference between "that was fixed last week" and a real bug. It updates by itself whenever your company deploys an update — there is nothing to refresh and nothing you can change here.

**How the number goes up:** a release that adds something new raises the middle number (`0.1.0` → `0.2.0`); a release with only fixes raises the last one (`0.2.0` → `0.2.1`). The first number changes only for a major milestone such as go-live (`1.0.0`).

If the popup says **`dev build`** instead of a date, you are on a test or development copy rather than a released one. On your company's live system it will always show a date.

## 12. If You Get Logged Out or See an Error

- If you haven't used the system in a while, you may be logged out automatically for security (either the account-wide session limit, or your own shorter auto-logout timer from §11) — just log back in.

### 12.1 How to read an error message

Every error dialog has up to three lines, and they mean different things:

1. **The headline** — what went wrong in plain words ("Required value is missing.").
2. **The detail** — the specific thing ("Field \"HSN Code\" (hsn_code) is required"). This is the line that usually tells you what to fix.
3. **What to do** — the suggested next step ("Enter the missing value.").

A short line under the detail says whether anything was saved ("Nothing was saved."). A **Help** link at the bottom opens that code's entry in the in-app help.

Underneath there's a **code** like `GLOBAL-0001` and a **correlation ID**. Look the code up in **[error-codes.md](error-codes.md)** for a fuller explanation; give the correlation ID to your admin if you need help, because it lets them find exactly this event in the log.

### 12.2 The errors people hit most

| What you see | What it actually means |
|---|---|
| *"Cash opening is required before billing"* | No cashier session is open at this location. §4.0. |
| *"Please enter HSN Code to continue"* | The item is missing its HSN Code. Every item needs one, whatever its tax treatment. |
| *"Tax category is required for this item"* | The item hasn't said how it's taxed. Either give it a **GST Rate** above 0, or set **Tax Treatment** to Exempt / Nil-Rated / Zero-Rated. The same message appears if you picked a non-taxable treatment *and* left a rate above 0 on it — the two contradict each other. |
| *"item not found"* at the till | The SKU isn't recognised. Code, barcode and internal id all work — check for a typo, or whether the item exists at all. |
| *"Selected Vendor does not exist or is inactive"* | You typed a vendor that isn't in the list. Use the suggestions as you type, or create the vendor first (§8). |
| *"…cannot move from 'X' to 'Y'"* | The document can't jump to that status from where it is. The message lists the statuses it *can* move to. |
| *"…requires a reason_code"* | You're reversing a decision (revoking an approved leave, un-selecting a vendor quote). Give a reason and it will go through. |
| *"This sale requires manager approval"* | The discount is over your store's threshold. The sale waits in Approvals; nothing is charged yet. |
| *"You do not have permission…"* | Your role can't do that. See **[PERMISSION_MATRIX.md](../archive/legacy/permission-matrix.md)**, then ask an admin. |
| *"A Reorder Point Config with this Item and Location already exists"* (also any record that repeats a key) | Open the record it already has instead of adding a second. A refusal that is not about a format no longer says "Correct the field format."; its action says what fits (cancel or reverse instead of deleting, choose the return location). |
| *"No posted invoice on this order - nothing to credit"* | **Credit note** on an OMS order raised nothing: the order has no posted invoice (an unposted draft is voided, not credited). |
| A **Rate Limit** message | The server is protecting itself from too many requests in a short time — from a script, a stuck key, or clicking very fast through many reports or imports. Nothing is broken and nothing was lost: the menu and your company's logo stay as they were. Wait up to a minute and carry on. Refreshing the page again does not help - each refresh asks the server for everything once more. If you see it during ordinary work, tell your administrator which screen you were on. |
| *"Too many requests"* | You've run reports faster than the limit allows. The message says how long to wait. |

### 12.3 When a screen shows something that can't be right — Refresh vs Reset

Two buttons sit together at the top right of every screen. They are not two strengths of the same thing, and picking the wrong one is why a stale screen sometimes seems incurable.

| Button | What it does | Use it when |
|---|---|---|
| **Refresh** | Re-asks the server for labels and screen definitions. **Everything else is kept** — cached lists, your place on the page, and what you have typed (item, customer and location boxes excepted). | An administrator just renamed a field or added one, and you want it to appear. |
| **Reset** | **Throws the cached copy away** and rebuilds from the server: cached lists, remembered sidebar sections, the screen code itself, and all unsaved on-screen work. | A screen is showing something you know is wrong — a record you deleted, an old name, a list that will not update. |

**Reloading the page in your browser is not the same as Reset**, and this is the part that catches people out. Pressing F5 restarts the app against the *same* stored copies, so whatever was stale comes straight back. Reset is the only thing that drops those copies first.

**Neither Refresh nor F5 moves anything.** The left menu stays as it was (same section open, same scroll), the screen stays where you were scrolled, a list keeps its own scroll, and only a value that actually changed tints briefly so you can see it.

**What Reset never touches:**

- **Your login.** You stay signed in. (To sign out, use the account menu.)
- **Your theme.** Light/dark stays as you set it.
- **Anything saved on the server.** Reset only clears what this browser is holding. No record, no sale and no document is deleted.
- **Sales waiting to sync.** If any offline sales are still queued, **Reset refuses to run at all** and tells you so — those are completed sales that have not reached the server yet, not cached data, and clearing them would lose them. Let them sync first (§4.1a), then Reset.

Reset will warn you before it runs, and it discards anything unsaved on screen — including a part-built POS cart. If you are mid-sale, finish or abandon it deliberately first.

> **At the till specifically**, you usually want one of the POS screen's own buttons instead: **New Sale (Reset)** between customers (§4.1a), or **Reset Terminal** to start a bill again (§4.5). The header Reset is the bigger hammer, for when the application itself looks wrong.

---

## 13. Open a Shop and Make Your First Sale — the full worked example

Everything above is per-screen. This section is the opposite: one continuous path from an empty system to money in the till, so you can see how the pieces connect. Allow about half an hour.

**One login is enough while self-approval is allowed:** whoever raises the purchase order may approve it too. If **Who can approve their own documents** is *Nobody* - where a new client starts (49.10.6b) - you need a second login to approve it (ADMIN_SOP §B.10).

### Step 1 — Create the place you trade from

**Setup → Core → Location → New Location.** Give it a code (`MAIN`), a name, and set **Type = Store**. Save.

> This is the one record that makes a shop real to the rest of the system — every transaction points at a Location. §8.

### Step 2 — Create the supplier you buy from

**Procurement → Vendors → New Vendor.** Name and contact details. Save.

### Step 3 — Create something to sell

**Setup → Inventory → Item → New Item.** Fill in the name and code, and — this is the step people skip — the **HSN Code** and **GST Rate**. Both are required; the system refuses to save without them, because it can't price a sale it can't tax. Save.

> **Selling something that isn't taxed?** Unbranded grain, fresh produce, salt, books and exports are all sold at 0%, and a 0 in **GST Rate** on its own is still rejected — the system can't tell it apart from a rate you haven't filled in yet. Set **Tax Treatment** (next to GST Rate, in the **Price and tax** section) instead:
>
> | Tax Treatment | Use it for | GST Rate |
> |---|---|---|
> | **Taxable** | Everything ordinary. This is what you get if you leave the field alone. | Must be greater than 0 |
> | **Exempt** | Goods exempted by notification — fresh produce, unbranded grain. | Set to 0 for you |
> | **Nil-Rated** | Goods whose tariff rate is genuinely 0% — salt, certain cereals. | Set to 0 for you |
> | **Zero-Rated** | Exports and SEZ supplies made under LUT/bond. | Set to 0 for you |
>
> **HSN Code is still required on all four** — it goes on the invoice whatever the rate is, and the nil/exempt part of GSTR-1 is reported HSN-wise too. And you can't have it both ways: pick a non-taxable treatment and the item may not carry a GST Rate above 0.

At this point you have an item with **zero stock**. That's expected.

### Step 4 — Order some stock

**Procurement → Purchase Order.** Pick your vendor, target warehouse and location, enter the amount, and click **Create Draft**. Note that you don't type a PO number — it's issued on save (§6.1).

Click **Submit for Approval**.

### Step 5 — Approve it (as the *other* user)

Log in as your second account, go to **Financial Accounting → Approvals**, find the PO, and click **Approve**.

> If Approve is refused, you're logged in as the person who raised it. That's the check working. Use the other account.

### Step 6 — Receive the stock

Back as the first user: **Procurement → Goods Receipt**. Click **+ New Goods Receipt**, pick the **Vendor**, tick your approved PO and click **Load ticked POs** (or use **Receive goods** on the PO's row). Confirm the quantities that actually arrived, type the vendor's invoice number, date and total, and click **Save receipt**. Then work through the steps (§6.10): **Verify**, **Check prices**, type the **MRP** and **Save and post** each child - or **Post all** when everything matches.

**Now check Inventory → Stock on Hand.** The quantity should have gone up. If it hasn't, stop here — everything downstream depends on this having worked.

### Step 7 — Open the till

**POS → POS / Billing.** Check that the store at the top right of the screen is your shop (click it and pick the shop if you work at several), click **Open Session**, and enter the cash physically in the drawer. While the session is open you stay at that shop (§4.0).

> **Shop not in the list at the top?** You have not been given it (an administrator adds it in **Users → Stores**), or it is set to **Sellable = No** — open Setup → Core → **Location**, find it, set **Sellable = Yes** and save. See §4.0 step 2.

### Step 8 — Sell something

Scan the item, or type part of its **name** and press Enter and pick it from the list. Choose a payment mode — for **Cash**, type what the customer handed you into **Cash tendered** and read off the **Change due**. Click **Complete Sale**.

The till now **freezes on the finished bill** (§4.1a): the bill number, the lines, the total collected and the change. Click **Print Receipt** to see it on screen; **Print** there sends it straight to the till printer if one has been set up for that (§4.3), otherwise through the browser's print dialog. Click **New Sale (Reset)** when you are ready for the next customer.

### Step 9 — Check that everything moved on its own

You should not have to tell any other screen about that sale. Confirm it:

| Check | Where | What you should see |
|---|---|---|
| Stock went down | **Inventory → Stock on Hand** | Quantity reduced by what you sold |
| The sale was recorded | **Reports → Sales → Sales Register**, then **Run** | Your sale, with its total |
| The books balanced | **Financial Accounting → Trial Balance** | Set **As Of Date** to today — debits equal credits, and the status reads "Balanced trial ledger" |
| The money is owed to the vendor | **Reports → Procurement → Vendor Ledger**, then **Run** | Your purchase order against that vendor |

### Step 10 — Close the till

Back on **POS / Billing**, click **Close Session** and enter the counted cash. The system shows expected, counted, and the variance.

The expected cash is the X-report's own figure - opening float + cash sales - cash refunds + paid in - paid out - safe drops + cash advances taken - advances refunded - so a close never disagrees with the X-report (140.12). When the till's bills took card or UPI, the close also asks the **card terminal's** and the **UPI app's** totals and keeps their difference from what the bills say. A cash difference is posted when the till closes (or when a manager approves an over-tolerance close): a shortage to Dr 5295 Cash Short / Over, an excess to Cr 5295. **Reports -> Sales & POS -> Z-report (till close)** (R1033) shows each closed till: bills and bill range, the whole drawer, counted cash, variance, tenders and the declared card / UPI totals. On the X-report an advance or store credit paying a bill is its own tender line (**Advance**, **Store Credit**), not Gift Card.

That's the whole loop: buy → receive → sell → and the accounting follows by itself. Every other feature in this guide hangs off this skeleton.

---

## 14. Glossary

| Term | In plain English |
|---|---|
| **GST** | The government sales tax added to a sale, calculated automatically. |
| **GRN** | Proof that ordered stock actually arrived — "Goods Receipt Note." |
| **GL / Ledger** | The accounting record of every rupee moving in or out of the business. |
| **SKU / Barcode** | The unique code identifying one specific product. |
| **Sticker Template** | A label layout you design once in the Sticker Studio, so the items it is set for (by item, item type, category or as the default — §7A.5) print with the right size and content (§7A.2). |
| **MFA** | A second security check (a code from your phone) in addition to your password. |
| **Approval / Maker-checker** | A rule that important actions need a second person to say yes, so no one person can make a big mistake (or fraud) alone. |
| **Tenant** | Your business's own private copy of the system — other businesses using the same system can never see your data. |
| **Role** | What kind of user you are (Cashier, Manager, Admin, etc.) — it decides what you can see and do. |
| **Sales Invoice** | The bill **you** give a customer — money owed **to** you. |
| **Vendor Invoice** | The bill a **supplier** gives you — money **you** owe. It is checked against the Purchase Order (what you ordered) and the Goods Receipt (what arrived) before it is paid: the "three-way match". |
| **Debit Note** | Sent **to a supplier** to reduce what you owe them — goods returned, or they overcharged. |
| **Credit Note** | Given **to a customer** to reduce what they owe you — a return or a cancelled sale. Easy way to remember: debit note → supplier, credit note → customer. |
| **POS Profile** | The settings for one till (which shop it sells from and its defaults), so a cashier can open the till without re-entering them. |
| **RFQ** | "Request for Quotation" — asking several suppliers for a price before you order (§6.4). |
| **HSN / SAC code** | The government's number for a kind of goods (HSN) or service (SAC); it decides the GST rate. 4, 6 or 8 digits (§8.1.2). |
| **Rate limit** | A ceiling on how many requests the server accepts per minute, to protect it. See §12.2. |
| **Correlation ID** | A tracking code shown when something goes wrong, so support can find exactly what happened. |

---

*This system is under active development — not every feature described in the full product plan exists yet. If something you expect to see isn't there, it may not be built yet rather than something you're doing wrong; ask your administrator.*

## 15. Run an import through staging

Use a staged import when a file is big or risky and you want to see what it will do before any record changes. Nothing is written to your records until you press **Commit**.

1. Open **Import Batch** from the record types list. The list shows every staged import, its step and its counts.
2. Press **New staged import**. Pick the record type and the CSV file, then **Stage**. A file over 5 MB, or one that is not text, is refused. Staging the same file again gives you the same batch, not a second one. For an opening-balance or other money file, type **Stated totals** (for example `amount=125000.50`): approval is refused (**DATAIM-0487**) while the file does not add up to them, and the batch shows Not balanced if the rows committed fall short. A reconciled batch also appears as a job in the import job list.
3. In the batch, press **Check**. Each row is marked Valid, Not valid or Duplicate, and each valid row is marked New or Update. Rows that repeat the same key inside the file are marked Duplicate. Use the Rows filter to see why a row was refused.
4. Press **Preview** to see what will be created, what will change (old value to new value) and the column totals.
5. Press **Approve**. If your company has an approval rule for Import Batch, a second person approves it (you cannot approve your own batch). With no rule, it is approved straight away.
6. Press **Commit**. Rows go in groups, so an interrupted commit can be pressed again: rows already in are not done twice. A row refused at this point is marked Failed and the rest still go in.
7. Press **Reconcile**. It compares the file, the rows committed and the records now in the system, and says Balanced or lists the difference.

If you change the import rules after checking, the batch says so: press **Check** again. **Discard** drops a batch that has not been committed. The raw rows of a finished batch are removed after the file-retention period set by your administrator; the batch record and its counts stay.
