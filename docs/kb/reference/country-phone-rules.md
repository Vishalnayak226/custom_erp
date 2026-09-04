---
title: Country codes and phone number rules
section: Reference
order: 30
summary: The countries the application knows, their dialling codes, and the phone number lengths each one accepts.
audience: admin, store manager
last_verified: 2026-09-03
screens: [configuration]
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: go run ./cmd/gendocs && go run ./cmd/genkb -->

# Country codes and phone number rules

The application enforces phone number shape per country. **54** countries are
known. Your tenant's default is set once under **Settings » Configuration**, in
`localization.default_country`, and every phone field in the system follows it
unless the number itself carries a different dialling code.

## How a number is read

Two separate things happen, and it is worth knowing which is which:

**Cleaning** always happens and never rejects anything. Spaces, hyphens, dots,
brackets and unicode dashes are removed; a leading `+91` or `0091` style prefix is
resolved to its country; a national trunk prefix such as a leading `0` is dropped.

**Validation** is then a policy the screen applies to the cleaned result.
Master records are strict - a Customer must have a number that fits its country.
Orders are not: a sales order records and tags whatever number it is given and
never refuses an order over it, because a contact number cannot tell you whether
an order can be fulfilled.

> [!NOTE]
> A dialling code is only stripped when what remains is a valid length for that
> country. A real Indian mobile number beginning `91` therefore survives intact
> rather than being read as the country code and truncated.

## The table

**Accepted lengths** is the number of digits after the country code and trunk
prefix are removed. **Trunk prefix** is the digit callers dial before a national
number, which the application removes if present.

| Country | Code | Dials | Accepted lengths | Trunk prefix | Example |
|---|---|---|---|---|---|
| Argentina | `AR` | +54 | 10 | `0` | 1123456789 |
| Australia | `AU` | +61 | 9 | `0` | 412345678 |
| Austria | `AT` | +43 | 10 or 11 | `0` | 6641234567 |
| Bahrain | `BH` | +973 | 8 | `none` | 36001234 |
| Bangladesh | `BD` | +880 | 10 | `0` | 1712345678 |
| Belgium | `BE` | +32 | 8 or 9 | `0` | 470123456 |
| Brazil | `BR` | +55 | 10 or 11 | `0` | 11912345678 |
| Canada | `CA` | +1 | 10 | `1` | 4165551234 |
| Chile | `CL` | +56 | 9 | `none` | 912345678 |
| China | `CN` | +86 | 11 | `none` | 13812345678 |
| Colombia | `CO` | +57 | 10 | `none` | 3211234567 |
| Denmark | `DK` | +45 | 8 | `none` | 20123456 |
| Egypt | `EG` | +20 | 10 | `0` | 1001234567 |
| Finland | `FI` | +358 | 9 or 10 | `0` | 451234567 |
| France | `FR` | +33 | 9 | `0` | 612345678 |
| Germany | `DE` | +49 | 10 or 11 | `0` | 15112345678 |
| Hong Kong | `HK` | +852 | 8 | `none` | 51234567 |
| India | `IN` | +91 | 10 | `0` | 9876543210 |
| Indonesia | `ID` | +62 | 9 or 10 or 11 | `0` | 81234567890 |
| Ireland | `IE` | +353 | 9 | `0` | 851234567 |
| Israel | `IL` | +972 | 9 | `0` | 501234567 |
| Italy | `IT` | +39 | 9 or 10 | `none` | 3123456789 |
| Japan | `JP` | +81 | 10 | `0` | 9012345678 |
| Kenya | `KE` | +254 | 9 | `0` | 712345678 |
| Kuwait | `KW` | +965 | 8 | `none` | 50123456 |
| Malaysia | `MY` | +60 | 9 or 10 | `0` | 123456789 |
| Mexico | `MX` | +52 | 10 | `none` | 5512345678 |
| Nepal | `NP` | +977 | 10 | `none` | 9841234567 |
| Netherlands | `NL` | +31 | 9 | `0` | 612345678 |
| New Zealand | `NZ` | +64 | 8 or 9 | `0` | 211234567 |
| Nigeria | `NG` | +234 | 10 | `0` | 8021234567 |
| Norway | `NO` | +47 | 8 | `none` | 40612345 |
| Oman | `OM` | +968 | 8 | `none` | 92123456 |
| Pakistan | `PK` | +92 | 10 | `0` | 3012345678 |
| Philippines | `PH` | +63 | 10 | `0` | 9171234567 |
| Poland | `PL` | +48 | 9 | `none` | 512345678 |
| Portugal | `PT` | +351 | 9 | `none` | 912345678 |
| Qatar | `QA` | +974 | 8 | `none` | 33123456 |
| Russia | `RU` | +7 | 10 | `8` | 9123456789 |
| Saudi Arabia | `SA` | +966 | 9 | `0` | 512345678 |
| Singapore | `SG` | +65 | 8 | `none` | 81234567 |
| South Africa | `ZA` | +27 | 9 | `0` | 821234567 |
| South Korea | `KR` | +82 | 9 or 10 | `0` | 1012345678 |
| Spain | `ES` | +34 | 9 | `none` | 612345678 |
| Sri Lanka | `LK` | +94 | 9 | `0` | 712345678 |
| Sweden | `SE` | +46 | 7 or 8 or 9 | `0` | 701234567 |
| Switzerland | `CH` | +41 | 9 | `0` | 781234567 |
| Taiwan | `TW` | +886 | 9 | `0` | 912345678 |
| Thailand | `TH` | +66 | 9 | `0` | 812345678 |
| Turkey | `TR` | +90 | 10 | `0` | 5321234567 |
| United Arab Emirates | `AE` | +971 | 9 | `0` | 501234567 |
| United Kingdom | `GB` | +44 | 10 | `0` | 7400123456 |
| United States | `US` | +1 | 10 | `1` | 2125551234 |
| Vietnam | `VN` | +84 | 9 | `0` | 912345678 |

## Currencies are not on this list, and why

Country and phone rules are fixed facts about the world, so the application
carries them in code. Currencies are not: which currencies you trade in, what you
call them, how many decimals you round to and what you are willing to accept as
an exchange rate are all business decisions. They are therefore **records you
create**, under **Setup » Currency** and **Setup » Exchange Rate**, not a list
shipped with the software. Use the ISO 4217 three-letter code as the currency's
code - `INR`, `USD`, `AED` - so that anything you later import or export lines
up.

Indian states are the same kind of thing and live on the records that need
them: a GSTIN's first two digits are the state code, which is how the application
decides whether a sale is intra-state or inter-state without asking anyone.
