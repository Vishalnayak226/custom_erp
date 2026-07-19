# Live connector verification (Stage 17.11)

`micro_checklist.md` 17.11 requires verifying each real channel connector (Shopify, BigCommerce, Magento/Adobe Commerce) against a real disposable store, one at a time, before upgrading that connector's Stage 16 status from BUILT to DONE. This needs real, non-production platform credentials, which only the account owner can supply — everything else (the connector code itself, and the verification tooling below) is built and ready.

## What's already built (code-complete, per `docs/ai_handover.md` §6)

- `engines/connector_shopify.go`, `connector_bigcommerce.go`, `connector_magento.go` — real API calls, each unit-tested against an `httptest.Server` standing in for the platform (`engines/connector_platforms_test.go`) so the request/response contract is verified without touching a live store.
- Credential storage: AES-256-GCM encrypted at rest (`engines/connector.go`'s `encryptChannelCredential`), saved via `POST /api/v1/pim/channels/{code}/credentials` (HR/Admin only).
- The full publish pipeline (readiness check → queue → background worker → connector call → log/retry) — Stage 15.2/16.1, unchanged by this stage.
- `scripts/verify_connector_live.ps1` — drives one real verification run: creates a disposable Channel, maps the item's category to it, saves real credentials, checks readiness, triggers a real publish, polls to a terminal status, reports the platform's own external id (proof it hit the real API, not the stub), then cleans up the Channel/ChannelCategoryMap it created.

## What only you can supply

1. **Non-production credentials** for whichever platform(s) you want to verify — a Shopify dev/partner store, a BigCommerce sandbox store, or a Magento Open Source/Adobe Commerce instance you control. Never use production/live-store credentials for this.
2. **An HR/Admin session token** — channel credential configuration requires that role (`internal/server/handlers_pim_pos_finance.go`'s `handleSaveChannelCredential`), and HR/Admin is MFA-gated in this build. The script does not automate login/MFA (that would mean storing a live TOTP secret in a script-readable file for a one-time run — not worth the trade). Log in through the app normally, complete MFA, and copy the Bearer token.

## Credentials file shape

Create `CONNECTOR_CREDENTIALS.local.json` at the repo root (gitignored — see `.gitignore`'s `*.local.json` entry, same convention as `DEV_CREDENTIALS.local.txt`). Only include the platform(s) you're verifying:

```json
{
  "Shopify": {
    "access_token": "shpat_...",
    "shop_domain": "your-dev-store.myshopify.com"
  },
  "BigCommerce": {
    "access_token": "...",
    "store_hash": "abc123"
  },
  "Magento": {
    "base_url": "https://your-magento-instance.example.com",
    "access_token": "...",
    "store_view_code": "default",
    "auth_mode": "OpenSource"
  }
}
```

Field names match exactly what each connector reads (`cred["..."]` in `engines/connector_shopify.go` / `connector_bigcommerce.go` / `connector_magento.go`) — nothing is renamed or reshaped in between.

## Procedure

1. **Prep a disposable test Item**, via the normal PIM Workbench UI (not scripted — see `scripts/verify_connector_live.ps1`'s header comment for why): fill `name`/`barcode`/`category`/`hsn_code`/`gst_rate`, and get its `ProductContent` to Approved for locale `en` through the normal editorial + approval flow. Use obviously-disposable values (e.g. item code `TEST-DISPOSABLE-001`, name "Stage 17.11 Verification - DO NOT SELL").
2. Log in as HR/Admin, complete MFA, copy the Bearer token.
3. Create `CONNECTOR_CREDENTIALS.local.json` per the shape above.
4. Run:
   ```powershell
   .\scripts\verify_connector_live.ps1 -Platform Shopify -ItemCode TEST-DISPOSABLE-001 -Token <bearer token>
   ```
5. Read the output. `SUCCESS` reports the platform's own external id (a Shopify GID, a BigCommerce numeric id, or a Magento SKU) — that's the proof the connector reached the real platform. `FAILED` reports the platform's own rejection message.
6. **Manually delete the resulting test product from the platform's admin panel.** The connectors are publish-only by design (no delete method) — see `engines/connector.go`'s interface. The script deletes its own disposable Channel/ChannelCategoryMap automatically but cannot reach into Shopify/BigCommerce/Magento to remove what it created there.
7. Repeat per platform, then update `micro_checklist.md` 17.11: move the verified platform from BUILT to DONE and record the date, store, and external id observed (redact the credential, keep the id — it's not sensitive, it's proof).

## Deferred, explicitly out of scope for this verification pass

Per the existing 17.11 checklist entry: Shopify variant grouping (ERP parent+variant → one Shopify product with real variants), Magento/Adobe Commerce token refresh, and full BigCommerce/Magento order-import pipelines. These are separate build items, not blocked on credentials the way this verification pass is.
