# Market Intelligence & Marketplace Crawling — Reference Notes

**Source project:** `Antigravity Projects/Buying Catalog/OmniCore` — a standalone Python microservices stack (FastAPI + Streamlit + SQLAlchemy/SQLite + Redis/RQ, wired by `docker-compose.yml`), five services: `crawler`, `pims`, `oms`, `erp`, `pos`. Last commit 2025-12-27; read in full and retired 2026-08-05.

**Why this file exists:** the same situation as `wms_master_blueprint_reference.md` — a separate project was started against ideas this repo also cares about, its architecture conflicts with this repo's standing rules (`CLAUDE.md`: no new framework, no new third-party dependency, one lightweight Go server — that stack needs Python + Playwright + BeautifulSoup + TextBlob + Redis), so it was retired rather than merged as code. This file is **the durable knowledge kept from it**. Its GitHub remote (`vishalnayak-design/Buying-Catalog`) no longer exists and 6 of its 7 commits were never pushed, so nothing below is recoverable from anywhere else.

**Headline finding:** four of the five services were strictly-inferior duplicates of what this repo already ships, so nothing was adopted:

| OmniCore service | Already covered here by |
|---|---|
| `erp/` — 138-line FastAPI + Streamlit UI, `finance_agent.py` | the entire ERP |
| `pims/` — SKU/design-code gen, EAN-13 barcode, variants | Phase 26.4 PIM sprint, `engines/pim_media.go`, EAN/barcode in `engines/inventory.go` |
| `oms/sync_engine.py` — reserve stock on marketplace order | Phase 26.12 OMS sprint, `engines/orders.go` (with real row-locking, not `print()` stubs) |
| `oms/marketplace_poller.py` — "SP-API / Flipkart Seller API" | nothing to adopt: it is a `random.random() < 0.1` **simulation**, zero real API knowledge |
| `pos/` — 121-line Streamlit | full POS incl. one-click print (Stage 31.1.9) |

The one genuinely net-new *concept* is **competitor/market price intelligence**. `engines/marketplace.go` here covers courier serviceability, shipping labels/manifests, delivery/RTO events and settlement reconciliation — it does **not** do catalog or competitor-price harvesting. Nothing below is backlogged; this is reference material for if that is ever asked for.

---

## 1. Per-platform product extraction (the actual asset)

Selectors were current as of Dec 2025 and **rot fast** — one of the retired project's last commits was literally "Fix: Myntra scrapping logic with multiple fallback keys". Treat the *strategies* as durable and the *strings* as expired.

**Myntra — parse JSON, not DOM.** Myntra ships its whole search result set as an embedded state blob. This is the single most valuable finding in the project, because it is immune to CSS churn:

```
window\.__myx\s*=\s*({.*?});          # primary
window\.__m_initial_state\s*=\s*({.*?});   # fallback
```

`json.loads` the capture, then `searchData.results.products[]`, each with `productName`, `price`, `rating`, `ratingCount`, `searchImage`. The retired crawler persisted the raw JSON as `page_N.json` (all other platforms got `page_N.html`) and kept a `li.product-base` DOM parser only as a last-ditch fallback.

**The others needed layered CSS fallbacks** — always 2-3 per field, because any single selector fails on a merchandised or A/B-tested grid:

| Platform | Container strategies (in order) |
|---|---|
| Amazon | `div[data-component-type='s-search-result']` → `div.s-result-item[data-asin]` → `div.zg-item-immersion` (best-seller grids) |
| Flipkart | `div._1AtVbE`, title via `div._4rR01T` / `a.s1Q9rs` / `a.IRpwTa` |
| Meesho | `div[class*="ProductCard__ProductCardWrapper"]` → `a[href*="/p/"]` — matched on *class substring*, since the styled-components hashes change every deploy |
| eBay | `li.s-item`, skipping the "Shop on eBay" placeholder row |

Two reusable details: Amazon's product URL is reconstructible as `https://www.amazon.in/dp/{data-asin}` rather than scraped from the anchor; ratings arrive as `"4.5 out of 5 stars"`, so split on `"out of"`, and review counts as `"1.2k"`, so handle the `k` suffix before `int()`.

## 2. Two-phase harvest → offline parse (the reusable architecture)

The best engineering idea in the project, and applicable to any scrape/ETL job regardless of language:

- **Phase 1 (network):** fetch raw pages in parallel under an `asyncio.Semaphore(5)` and write each straight to `raw/page_N.html` untouched. Before fetching, **skip any page whose file already exists and is >100 bytes** — that one line gives free resumability *and* a cache, so an interrupted 50-page crawl resumes where it stopped.
- **Phase 2 (no network):** parse from disk. Parser bugs become re-runnable at zero network cost and zero re-ban risk — the single biggest time sink in scraping is re-fetching to test a selector fix.
- Media is aborted at the network layer for speed: route-block `media`, `font`, `stylesheet` resource types.

Anti-bot posture used: `--disable-blink-features=AutomationControlled`, a randomised UA, `locale=en-IN` + `timezone_id=Asia/Kolkata`, and an init script setting `navigator.webdriver` to `undefined`. Optional upstream proxy via a `PROXY_URL` env var. Deep-crawl passes slept a random 2-4 s between product pages.

**Pagination is per-platform:** Amazon/Flipkart `&page=N`, Myntra `?p=N` (regex-replace an existing `p=`), eBay `&_pgn=N`.

## 3. Enrichment ideas worth keeping

- **Pre-crawl category detection.** Before harvesting, visit page 1 and read breadcrumbs (`#wayfinding-breadcrumbs_feature_div ul li a` on Amazon, `.breadcrumbs-link` on Myntra) — the last crumb is the category. Fall back to splitting the `<title>` on `:` / `-` / `|`. Cheap way to auto-file a crawl without asking the user to type a category.
- **Deep product pass.** For Amazon, follow `/dp/{asin}` for MRP (`.a-text-price .a-offscreen`), availability, and a spec table (`#technicalSpecifications_section_1 tr` → `#prodDetails tr` fallback), then `/dp/` → `/product-reviews/` for review bodies (`span[data-hook='review-body']`). MRP-vs-price is what makes discount-depth analysis possible.
- **Quality filter before storage,** with an escape hatch: it kept only rating ≥ 4.2 and reviews ≥ 20, but if that left zero rows it kept everything rather than reporting an empty crawl. Good defensive shape for any threshold filter.
- **Dedupe on write, not on read.** `save_results` created a new crawl-run row every time (preserving history) but skipped products whose name already existed in that category — updating the in-memory seen-set as it went so duplicates *within* one batch were caught too.

## 4. AI-agent patterns (if this repo ever adds AI extraction)

Both used `gemini-1.5-flash` via `google.generativeai`, keyed off a `GEMINI_API_KEY` env var, with a mock-response path when the key was absent — a decent pattern for keeping a feature testable without credentials.

- **Invoice OCR** (`erp/finance_agent.py`): image → prompt asking for invoice number, vendor, date, total, line items → JSON. Response cleanup stripped ` ```json ` fences before `json.loads`, and on parse failure returned `{"raw_text": ..., "error": ...}` rather than throwing.
- **Product-image attribute extraction** (`pims/pims_core.py`): image → Category/Color/Material/Pattern/Type.
- **The pattern worth copying is the QA gate.** `validate_attributes()` rejected any result whose `Category` or `Color` came back `"Unknown"`, checked for missing required keys, and stamped `QA_Status: Passed|Failed|Error` onto the stored record. The model's output was treated as a claim to be validated, never as fact. Any AI extraction added here should carry the same gate and the same stored status field.

## 5. Marketplace listing-template fill

`pims/marketplace_agent.py`, ~55 lines: read a marketplace's XLSX seller template, take its header row, fuzzy-match each header to an internal field (`'sku' in h.lower()`, `'price'|'mrp'`, `'stock'|'quantity'`, …), append the mapped rows, save as `Filled_<template>.xlsx`. Trivial implementation but the right *shape* for a future "export catalog to marketplace template" feature — and the retired DB had a `marketplace_mappings` table (platform, attribute_name, internal_value, marketplace_value) to make the mapping data-driven per channel rather than hardcoded. If ever built here, it belongs in the existing report/`BulkImportCSV` conventions, not a new subsystem.

## 6. Do not resurrect the code blind

Independent of the architectural mismatch, the retired crawler did not work end-to-end:

- `crawler.py`'s `crawl()` referenced an undefined `keep_raw` in its cleanup block — **NameError at the end of every otherwise-successful crawl**, after all the work was done.
- `api.py` called `run_crawl(url, platform, category, max_pages, callback, req.keep_raw)` — six arguments into a five-parameter function. **TypeError on every API-triggered crawl.**
- The Flipkart branch of Phase 2 was a bare `pass` with a comment admitting the offline-parse architecture couldn't run its Playwright-based parser. Flipkart parsing was dead.
- Two competing job systems coexisted: an RQ/Redis worker (`worker.py`, `get_current_job()`) *and* FastAPI `BackgroundTasks` with an in-memory `jobs` dict.
- `crawler.py` and `start_service.py` `pip install`-ed dependencies at import/startup time ("self-healing"). Convenient in a scratch project, unacceptable anywhere near production — it makes the runtime environment unreproducible.

The `cto_bot.py` "Active Guardian" (psutil resource sampling, `/health` polling, restart-on-death supervision, debug-artifact cleanup) was a reasonable in-repo process supervisor for a laptop-run stack, but it duplicates what any real supervisor or `docker-compose restart:` policy does — and this repo's control plane (Stage 14) already covers health/monitoring properly.
