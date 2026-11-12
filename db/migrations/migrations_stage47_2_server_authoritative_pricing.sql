-- ---------------------------------------------------------------------------
-- Stage 47.2 - server-authoritative POS quote, price, tax, discount and cost
-- (audit finding A-02/A-38).
--
-- The hole this closes, exactly: until now the ONLY source of a POS line's
-- sale price and cost price was the browser (public/app.js pushes every cart
-- line with `salePrice: 0, costPrice: 0` and lets the cashier type both), and
-- handleCheckout gated discount approval on a separate, unrelated, equally
-- client-supplied `discount_pct`. So the same money could be rung up two ways
-- - declared as a discount (gated) or silently as a low price (not gated) -
-- and the cost posted to COGS was whatever the client said it was.
--
-- Nothing here is destructive and nothing changes an existing tenant's data:
-- three optional Item fields, one optional Customer field, and two read-only
-- evidence doctypes. Item.sale_price is the master price the quote engine
-- looks for; a tenant that never fills it in keeps working as today
-- (pos.pricing_mode defaults to 'assisted' - see engines/settings_definitions.go)
-- while still gaining the real fix, because the approval gate now derives the
-- discount from whatever server reference DOES resolve rather than trusting a
-- client-declared percentage.
--
-- No new price store: the effective-dated price list is Stage 37.6.4's
-- existing PriceListVersion doctype and its ResolvePriceForSKU resolver,
-- which had no production caller until this stage.
-- ---------------------------------------------------------------------------

-- 47.2.1 - the master price fields the quote resolver reads. display_order 44+
-- continues after Stage 42.3.9's gtin/country_of_origin/hazmat_class (41-43),
-- so the Item form's existing field order is untouched.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'sale_price', 'Sale Price (incl. GST)', 'Number', FALSE, NULL, 44),
('Item', 'mrp', 'MRP (incl. GST)', 'Number', FALSE, NULL, 45),
('Item', 'standard_cost', 'Standard Cost (ex-GST)', 'Number', FALSE, NULL, 46)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 47.2.1 - "customer/contract/price list" as a quote input. A Customer with
-- no price_list_code resolves through the tenant default price list instead,
-- so this is optional for every existing customer.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'price_list_code', 'Contract Price List (optional)', 'Data', FALSE, NULL, 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 47.2.3 - price override as a separate, capability-gated command with its own
-- immutable evidence, instead of "type a lower number into the price box".
--
-- Written only by engines/pos_quote.go's RecordPriceOverride - the same "no
-- role gets a generic create grant, every write goes through dedicated engine
-- code" pattern as POSSession/POSOfflineSyncVariance (see
-- migrations_stage20_13_offline_pos_sync.sql for why). Registered as a
-- doctype so the existing generic doctype-table screen, the maker-checker
-- approval engine and the audit trail all work on it with no new plumbing.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('POSPriceOverride', 'Sales', 'Transaction', 'sales')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSPriceOverride', 'cart_number', 'Cart Number', 'Data', TRUE, NULL, 1),
('POSPriceOverride', 'sku', 'SKU', 'Data', TRUE, NULL, 2),
-- Mandatory: an override always belongs to one store's till, and 47.1.4's
-- location scope is what keeps one store's overrides out of another's list.
-- A doctype whose location is optional is treated as unscoped by that policy
-- (engines/scope_policy.go), which would be the wrong answer for evidence of
-- a price deviation.
('POSPriceOverride', 'location', 'Location', 'Data', TRUE, NULL, 3),
('POSPriceOverride', 'reference_price', 'Server Reference Price', 'Number', TRUE, NULL, 4),
('POSPriceOverride', 'override_price', 'Approved Override Price', 'Number', TRUE, NULL, 5),
('POSPriceOverride', 'discount_pct', 'Effective Discount (%)', 'Number', TRUE, NULL, 6),
('POSPriceOverride', 'reason', 'Reason', 'Data', TRUE, NULL, 7),
('POSPriceOverride', 'requested_by', 'Requested By', 'Data', TRUE, NULL, 8),
('POSPriceOverride', 'approved_by', 'Approved By', 'Data', FALSE, NULL, 9),
('POSPriceOverride', 'quote_version', 'Quote Version', 'Data', FALSE, NULL, 10),
('POSPriceOverride', 'status', 'Status', 'Select', TRUE, 'Approved,Pending Approval,Rejected,Consumed', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Self-correcting follow-up for `location`, which is deliberately NOT covered
-- by the ON CONFLICT DO NOTHING above.
--
-- The reason is specific: during development this file first declared location
-- optional, and any database that applied THAT version keeps `mandatory =
-- FALSE` for ever, because DO NOTHING will not update an existing row. A
-- database in that state reads as unscoped to engines/scope_policy.go, which
-- would make one store's price-override evidence visible at every other store
-- - the opposite of what this doctype exists for. Only the local dev database
-- ever saw the earlier version, and it was corrected by hand; this statement
-- is what makes the correction part of the migration instead of a fact about
-- one machine, so a restored snapshot or a re-provisioned tenant cannot
-- silently inherit the weaker setting.
UPDATE tenant_default.doctype_fields
   SET mandatory = TRUE
 WHERE doctype_name = 'POSPriceOverride' AND fieldname = 'location' AND mandatory IS DISTINCT FROM TRUE;

-- Read-only everywhere. Creation is the capability-gated command
-- (POST /api/v1/pos/price-override), not a generic doctype create, and the
-- engine is the only writer - exactly so an override cannot be minted by
-- POSTing a document. Cashier gets read so the POS screen can show the
-- override that was granted for the cart in front of them.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSPriceOverride', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'POSPriceOverride', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'POSPriceOverride', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 47.2.2 - POSCostingGap: the honest record of a sale whose COGS could not be
-- resolved from any server-side source. Before this stage the client's
-- cost_price silently filled that hole; removing it must not make the gap
-- invisible, so every zero-cost line is recorded here for finance to fix
-- (by receiving the item through a GRN, or setting Item.standard_cost).
-- Same read-only/engine-written shape as POSOfflineSyncVariance above.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('POSCostingGap', 'Sales', 'Transaction', 'sales')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSCostingGap', 'cart_number', 'Cart Number', 'Data', TRUE, NULL, 1),
('POSCostingGap', 'sku', 'SKU', 'Data', TRUE, NULL, 2),
('POSCostingGap', 'qty', 'Qty Sold', 'Number', TRUE, NULL, 3),
('POSCostingGap', 'sale_value', 'Sale Value', 'Number', TRUE, NULL, 4),
('POSCostingGap', 'status', 'Status', 'Select', TRUE, 'Open,Resolved', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSCostingGap', TRUE, FALSE, TRUE, FALSE),
('Store Manager', 'POSCostingGap', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 47.2.1/47.2.4 - the quote index. ResolvePOSQuote reads PriceListVersion by
-- (price_list_code, effective window) on every cart line; ResolvePriceForSKU
-- had only ever been called from a test before this stage, so the query it
-- has always issued has never had an index. Partial on the two statuses the
-- resolver actually accepts (see ResolvePriceForSKU's own comment on why
-- 'Superseded' is included and Draft/Rejected are not).
CREATE INDEX IF NOT EXISTS idx_price_list_version_lookup
  ON tenant_default.documents ((data->>'price_list_code'), (data->>'effective_from') DESC)
  WHERE doctype = 'PriceListVersion' AND deleted_at IS NULL AND status IN ('Approved', 'Superseded');

-- 47.2.3 - the override lookup ResolvePOSQuote does once per cart.
CREATE INDEX IF NOT EXISTS idx_pos_price_override_cart
  ON tenant_default.documents ((data->>'cart_number'))
  WHERE doctype = 'POSPriceOverride' AND deleted_at IS NULL;
