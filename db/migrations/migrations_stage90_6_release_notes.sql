-- Stage 90.6: the Control Panel's Releases table says, in a few words, what
-- each release changed (user, 2026-10-24: "very small"). Rewrites the notes of
-- the releases recorded before the deploy wrote them itself; each row is
-- matched by version + commit, so on any other database this changes nothing.
UPDATE public.deployments SET notes = 'Job work, e-way bills, challans · Client branding · Sticker reprints'
WHERE app_version = '0.13.0' AND git_commit LIKE '1ae3c79%';
UPDATE public.deployments SET notes = 'CRM · Store app · Bulk purchase upload · Shopify'
WHERE app_version = '0.12.0' AND git_commit LIKE '1356183%';
UPDATE public.deployments SET notes = 'Setup Workbook · Silent printing · One login for all clients'
WHERE app_version = '0.11.0' AND git_commit LIKE 'd5d7668%';
UPDATE public.deployments SET notes = 'Purchase order setup guide'
WHERE app_version = '0.10.0' AND git_commit LIKE '54b69b9%';
UPDATE public.deployments SET notes = 'Wholeops logo · Sticker print options · Frozen item-field headers'
WHERE app_version = '0.9.0' AND git_commit LIKE '7cfe141%';
UPDATE public.deployments SET notes = 'Leaner PO entry · MRP rules per product type · Names, not codes'
WHERE app_version = '0.8.0' AND git_commit LIKE 'f0a324f%';
UPDATE public.deployments SET notes = 'GRN follows PO GST · Plain-words Verify · Print on scan'
WHERE app_version = '0.7.0' AND git_commit LIKE 'f7acbdd%';
UPDATE public.deployments SET notes = 'PO entry: real items only, every attribute shown'
WHERE app_version = '0.6.0' AND git_commit LIKE '5d1329f%';
UPDATE public.deployments SET notes = 'One-click release and deploy · Daily server cleanup'
WHERE app_version = '0.5.0' AND git_commit LIKE '7ca4d4e%';
UPDATE public.deployments SET notes = 'Sticker presets · 3-inch POS bill · Exact-barcode POS'
WHERE app_version = '0.4.0' AND git_commit LIKE 'e9b183a%';
UPDATE public.deployments SET notes = '252 reports · Print, PDF, send any transaction · GRN in steps'
WHERE app_version = '0.3.0' AND git_commit LIKE '06547bb%';
UPDATE public.deployments SET notes = 'POS billing · Item master · Cost centers · Version numbers'
WHERE app_version = '0.2.0' AND git_commit LIKE '455f4f9%';
UPDATE public.deployments SET notes = 'First production release'
WHERE app_version = '0.1.0' AND git_commit LIKE '840909b%';
