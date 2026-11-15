---
doc_id: DOC-CA74001A01
title: CA confirmation pack
type: record
status: draft
owner: operations-owner
approvers: [documentation-maintainer, operations-owner]
audience: [maintainers, operations-owner]
applies_to: DEC-03, DEC-10, DEC-14, DEC-15 and DEC-16 of tracker item 74.0.1, and the 50.12 invoice row
authority: proposed-policy
confidentiality: internal
last_verified: 2026-11-15
review_by: 2026-12-15
supersedes: none
superseded_by: none
verification_scope: code defaults compared with official text; statutory treatment is the CA's to confirm
---

# CA confirmation pack

Item 74.0.1. The product defaults were approved on 2026-10-18. The statutory treatment
below is not decided here: each row says what the code does, what the official text
says, and what the CA must confirm. Official text was read on 2026-11-15 from
taxinformation.cbic.gov.in and the GST portals. Nothing in this pack is tax advice.

| Decision | What the code does | What the official text says | CA must confirm |
|---|---|---|---|
| DEC-03 customer advance | `engines/customer_advance.go`: advance for goods carries no GST at receipt; advance for services is taxed at receipt and the tax is given back when the bill is raised or the advance refunded. | [Section 12](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_CGST_act/active/chapter4/section12_v1.00.html) (2) still reads "earlier of invoice or payment" for goods; Notification 66/2017-CT (15 Nov 2017) exempts tax on goods advances (text not re-read, only its title). Section 13(2)(a) taxes service advances at receipt. Rule 50 (receipt voucher, 18% when the rate is not known) and Rule 51 (refund voucher) set voucher content. The section 12 page notes the voucher provision of sub-section (4) was omitted from 1 Oct 2025 (Finance (No. 2) Act 2025, s.122). | (a) Notification 66/2017 still governs goods advances. (b) Whether the 1 Oct 2025 omission changes the receipt-voucher duty. (c) Tax split and giveback on an unused services advance. |
| DEC-10 job work | `engines/job_work.go`: inputs due back in 1 year, capital goods in 3, counted from the send date, or from the optional "Received by job worker on" date when one is entered (built 2026-11-15, CA to confirm); e-way bill needed for inter-State job work at any value. | [Section 143](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_CGST_act/active/chapter21/section143_v1.00.html): 1 year inputs, 3 years capital goods; sub-sections (3) and (4) count the period from the goods being "sent out" and the section as published does not use the word "receipt"; the Commissioner may extend on sufficient cause. [Rule 45(3)](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/rules/cgst_rules/active/chapter5/rule45_v1.00.html): ITC-04 half-yearly (periods from 1 April and 1 October) if prior-year turnover is over 5 crore, else yearly; due on the 25th after the period. [Rule 138(1)](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/rules/cgst_rules/active/chapter16/rule138_v1.00.html) proviso: inter-State job work needs an e-way bill whatever the value. | (a) Clock for goods sent straight to a job worker: the code counts from the receipt date when one is entered, else the send date. The published section says "sent out" and gives no receipt rule, so confirm the source for counting from receipt (a circular or FAQ) or drop the date. (b) The ITC-04 period and the 5 crore test used by the return screen. (c) Any extension notified for the year. |
| DEC-14 IRP route | An adapter (`engines/einvoice.go`) lets the direct IRP and a GSP be swapped; turnover band is held per GSTIN as 5-10CR or 10CR+. | e-invoicing applies above 5 crore AATO (GSTN advisory, Apr 2025). | (a) Direct IRP or GSP per entity. (b) The AATO test: the code stores a band the user picks, not the statutory AATO. |
| DEC-15 IRN when offline | `engines/einvoice_controls.go`: a 10CR+ GSTIN's document gets alerts at 7, 25 and 28 days; the IRP refuses it after 30 days. | [GSTN advisory](https://einvoice6.gst.gov.in/content/revised-time-limit-for-e-invoice-reporting-for-businesses-with-aato-of-%E2%82%B910-crores-above/): from 1 April 2025, AATO of 10 crore and above cannot report an invoice older than 30 days; no limit below 10 crore. | (a) What may be done for an eligible invoice while the IRP is down. (b) The 30 days is an advisory, not a notification: confirm it holds. |
| DEC-16 e-way exceptions | `engines/ewaybill.go` and `engines/delivery_challan.go`, through one helper (`ewbValueNeedsBill`): bill needed when value is over `gst.ewb_threshold` (default 50,000); built to Rule 138(1) "exceeding" on 2026-11-15, tested at 50,000 and 50,001. Exempt HSN codes are a tenant setting (`gst.ewb_exempt_hsn`), empty by default, so no "silver is exempt" rule is built in. | Rule 138(1) says "exceeding fifty thousand rupees". Rule 138(14) exempts goods in its Annexure, non-motorised transport, and other listed cases. I could not read the Annexure from the CBIC page. | (a) Confirm "exceeding" is the right reading for the consignment value including tax. (b) The Annexure entries for precious metals and stones, and mixed consignments. (c) Which HSN codes to load as exempt per tenant. |
| 50.12 SaaS subscription invoice | `engines/saas_invoice_gst.go`: the platform's invoice for a Razorpay subscription payment is GST-inclusive at 18%, split CGST + SGST when the place of supply is the supplier's State and IGST otherwise (place of supply: the registered recipient's GSTIN State, else the address on record, else the supplier's State). Supplier GSTIN comes from `SAAS_SUPPLIER_GSTIN`, the SAC from the setting `saas.invoice_sac` (no default). With either blank the payment is held as pending with no invoice number. Serial `SA/26-27/000123` (15 characters). | [Rule 46 CGST Rules](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/rules/cgst_rules/active/chapter6/rule46_v1.00.html) lists the tax invoice particulars and the 16-character serial limit; [IGST Act section 12(2)](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_IGST_Act/active/chapterv/section12_v1.00.html) sets the place of supply of services. Both pages re-read 2026-11-15. The rule asks for the HSN code (SAC is the services form), a description of the service, and needs no signature on an electronic invoice. | (a) The SAC and the 18% rate for a SaaS subscription. (b) The Rule 46 particulars and the serial format against the current rule. (c) Place-of-supply order for an unregistered recipient. (d) Does e-invoicing (IRN) apply to these invoices at the business's turnover? |

## Rates the CA should also check (item 66.7)

The six HSN headings added to `engines/hsn_master.json` use Notifications 9/2025-CT(Rate),
10/2025-CT(Rate) and 14/2025-CT(Rate). Every serial was checked in the official Goa State Tax
gazette copies of the same notifications (statetax.goa.gov.in), which carry the CBIC serials;
the CBIC original was not retrieved. A mirror site (taxo.online) is not cited. Points:
3101 pre-packaged is Schedule I S.No.237 (not 249); 4418 bamboo joinery is S.No.303; AAC
blocks may sit under 6810 or 6904; fly ash bricks and blocks in the master sit under 6810 but
the notification places them under 6815 at 12%. Copies read: https://statetax.goa.gov.in/PDF/state_notif/Rate/09_(09_2025)_2025(rate).pdf, .../10_(10_2025)_2025(rate).pdf and .../14_(14_2025)_2025(rate).pdf. A rate not confirmed in those copies is not
loaded.

## Owner action

Send this page to the CA and record the signed answers against 74.0.1.
