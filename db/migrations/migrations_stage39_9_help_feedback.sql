-- Stage 39.9 - "Was this helpful?" feedback on a Knowledge Center article.
--
-- Stored as an ordinary generic document rather than a bespoke table, so
-- submission goes straight through the existing POST /api/v1/doc/{doctype}
-- API (internal/server/handlers_core_doc_engine.go) with no new Go handler at
-- all - the same reuse the LottableConstraint/Batch/SerialNumber doctypes
-- already demonstrate. `article` deliberately holds the article's *slug*
-- (Data, not Link): a Knowledge Center article is not itself a document in
-- this schema, so there is nothing for a Link to resolve against.
--
-- module_key 'core' (is_core = TRUE in public.modules, see
-- migrations_stage18_core_module_fix.sql) rather than a new module: the
-- Knowledge Center is authenticated-by-default platform functionality per
-- 39.6, not something a tenant opts into or out of, so this doctype must
-- never go dark just because some unrelated module toggle is off.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('HelpArticleFeedback', 'Core', 'core', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('HelpArticleFeedback', 'article', 'Article Slug', 'Data', TRUE, NULL, 1),
('HelpArticleFeedback', 'helpful', 'Was this helpful?', 'Select', TRUE, 'Yes,No', 2),
('HelpArticleFeedback', 'comment', 'Comment', 'Data', FALSE, NULL, 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Every internal role can submit feedback while reading the Knowledge Center
-- (create), but only Super Admin (still stored as 'HR/Admin' - Stage 40.3
-- canonicalises the display name, not this column) can browse raw
-- submissions or read them back individually; everyone else's signal reaches
-- an author only through the aggregate 39.9 report, not by browsing rows
-- that may carry a free-text comment. Supplier is deliberately left out:
-- handleGenericDoc's row-level supplier scoping expects a `vendor`-shaped
-- field this doctype has none of, and Knowledge Center feedback is an
-- internal-facing concern, not part of the supplier portal.
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'HelpArticleFeedback', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'HelpArticleFeedback', FALSE, TRUE, FALSE, FALSE),
('Cashier',        'HelpArticleFeedback', FALSE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;