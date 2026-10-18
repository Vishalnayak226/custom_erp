// Stage 81 (the user, 2026-10-18: "I will create variant for the client and it
// will be set for the client but disablable by client if needed ... I will
// share a master with predefined value"): the platform operator's page for one
// client's product attributes, opened from Control Panel -> Attributes.
//
// The industry attribute library (engines/attribute_library.json, generated
// from the operator's workbook) is listed in three parts:
//   - Variant attributes - what tells one sellable combination from another.
//     Offered on the Item's variant options, in PO designs and on prints.
//   - Product information - descriptive attributes for PIM enrichment.
//   - Not added - kept elsewhere in the ERP already, or recorded per batch /
//     serial unit rather than per item.
// The workbook's own Variant Driver column decides the default; the operator
// can tick, untick or move any row. Adding never touches what the client
// already has, so a value it removed or an attribute it switched off stays so.
//
// Native ES module, loaded on demand by view-control-panel.js; shared app
// services (apiFetch, escapeHTMLText, renderView, showCustomAlert...) resolve
// from the classic shell.

const USE_VARIANT = 'Variant';
const USE_PRODUCT = 'Product information';

export async function renderAttributeLibrary(container, client, industry = '') {
  const name = client.name || client.tenant_id;
  const url = `/api/v1/platform/tenants/${encodeURIComponent(client.tenant_id)}/attribute-library`;
  const res = await apiFetch(url + (industry ? `?industry=${encodeURIComponent(industry)}` : ''));
  if (!res) return;
  if (!res.ok) { await showApiError(res, `Could not load the attribute library for ${name}.`); return; }
  const lib = await res.json();
  const attrs = lib.attributes || [];

  const offerUse = a => (a.use === 'variant' || a.use === 'variant-candidate') ? USE_VARIANT : USE_PRODUCT;
  const ticked = a => !a.client && !a.similar && (a.use === 'variant' || (a.use === 'product' && lib.pim_enabled));
  const variantRows = attrs.filter(a => a.use === 'variant' || a.use === 'variant-candidate');
  const productRows = attrs.filter(a => a.use === 'product');
  const otherRows = attrs.filter(a => a.use === 'covered' || a.use === 'lot');
  const industryName = code => ((lib.industries || []).find(i => i.code === code) || {}).name || code;

  const valuesCell = a => {
    if (!(a.values || []).length) return `<span class="text-muted">${escapeHTMLText(a.type === 'Decimal' || a.type === 'Integer' ? `Number${a.uom ? ` (${a.uom})` : ''}` : a.type === 'Date' ? 'Date' : 'Typed')}</span>`;
    const shown = a.values.slice(0, 6).map(v => escapeHTMLText(v)).join(', ');
    return `${shown}${a.values.length > 6 ? ` <span class="text-muted">+${a.values.length - 6} more</span>` : ''}`;
  };
  const clientCell = a => {
    if (a.client) return `<span class="badge ${a.client.status === 'Inactive' ? 'badge-secondary' : 'badge-success'}">${a.client.status === 'Inactive' ? 'Has it · off' : 'Has it'}</span>`;
    if (a.similar) return `<span class="text-muted" title="Not ticked: the client already has a similar attribute">Similar: ${escapeHTMLText(a.similar.label)}</span>`;
    return '';
  };
  const row = a => `
    <tr${a.client ? ' class="text-muted"' : ''}>
      <td><input type="checkbox" data-al-pick="${escapeHTMLText(a.id)}" aria-label="Add ${escapeHTMLText(a.name)}"${ticked(a) ? ' checked' : ''}${a.client ? ' disabled' : ''}></td>
      <td><strong>${escapeHTMLText(a.name)}</strong><div class="text-muted" style="font-size:12px;">${escapeHTMLText(a.group || '')}${a.industry === 'CORE' ? ' · shared' : ''}</div></td>
      <td><select class="form-select" style="min-width:150px;" data-al-use="${escapeHTMLText(a.id)}"${a.client ? ' disabled' : ''}>
        ${[USE_VARIANT, USE_PRODUCT].map(u => `<option${(a.client ? (a.client.used_for || offerUse(a)) : offerUse(a)) === u ? ' selected' : ''}>${u}</option>`).join('')}
      </select></td>
      <td style="max-width:360px;">${valuesCell(a)}</td>
      <td style="max-width:260px; font-size:12px;" class="text-muted">${escapeHTMLText(a.note || '')}</td>
      <td>${clientCell(a)}</td>
    </tr>`;
  const table = (title, rows, id) => `
    <div class="table-panel" style="margin-bottom:16px;" data-al-section="${id}">
      <div style="display:flex; justify-content:space-between; align-items:center; gap:12px; padding:16px 16px 0;">
        <h3 style="font-size:16px; font-weight:600; margin:0;">${title} <span class="text-muted" style="font-weight:400;">(${rows.length})</span></h3>
        <span style="display:flex; gap:6px;">
          <button type="button" class="btn btn-ghost btn-sm" data-al-all="${id}">Tick all</button>
          <button type="button" class="btn btn-ghost btn-sm" data-al-none="${id}">Untick all</button>
        </span>
      </div>
      <div class="table-wrapper"><table>
        <thead><tr><th></th><th>Attribute</th><th>Use as</th><th>Values</th><th>Note</th><th>${escapeHTMLText(name)}</th></tr></thead>
        <tbody>${rows.length ? rows.map(row).join('') : '<tr><td colspan="6" class="text-muted">None for this industry.</td></tr>'}</tbody>
      </table></div>
    </div>`;

  const root = document.createElement('div');
  container.appendChild(root);
  root.innerHTML = `
    <div class="page-header">
      <div class="page-title-section"><h1 class="page-title">Product attributes - ${escapeHTMLText(name)}</h1></div>
      <div style="display:flex; gap:8px; align-items:center;">
        <button type="button" class="btn btn-outline" data-al-back>&larr; Control Panel</button>
      </div>
    </div>
    <div class="table-panel" style="padding:16px; margin-bottom:16px; display:flex; flex-wrap:wrap; gap:16px; align-items:flex-end;">
      <label class="form-label" style="display:flex; flex-direction:column; gap:4px;">Industry
        <select class="form-select" data-al-industry style="min-width:260px;">
          ${(lib.industries || []).map(i => `<option value="${escapeHTMLText(i.code)}"${i.code === lib.industry ? ' selected' : ''}>${escapeHTMLText(i.name)}</option>`).join('')}
        </select></label>
      <div style="font-size:13px; line-height:1.6;">
        <div>Client's industry: <b>${escapeHTMLText(lib.client_industry || 'not chosen')}</b> · PIM module: <b>${lib.pim_enabled ? 'on' : 'off'}</b></div>
        <div>Already has <b>${attrs.filter(a => a.client).length}</b> of these, and <b>${(lib.others || []).length}</b> attribute(s) of its own.</div>
      </div>
    </div>
    ${table('Variant attributes', variantRows, 'variant')}
    ${table('Product information (PIM)', productRows, 'product')}
    <details class="table-panel" style="margin-bottom:16px;">
      <summary style="padding:16px; cursor:pointer; font-weight:600;">Not added (${otherRows.length})</summary>
      <div class="table-wrapper"><table>
        <thead><tr><th>Attribute</th><th>Why</th></tr></thead>
        <tbody>${otherRows.map(a => `<tr><td>${escapeHTMLText(a.name)}</td><td class="text-muted">${a.use === 'covered' ? `Already in the ERP: ${escapeHTMLText(a.covered_by)}` : `Recorded per ${escapeHTMLText(String(a.scope || '').toLowerCase())} (batch / serial unit), not per item`}</td></tr>`).join('')}</tbody>
      </table></div>
    </details>
    ${(lib.others || []).length ? `
    <details class="table-panel" style="margin-bottom:16px;">
      <summary style="padding:16px; cursor:pointer; font-weight:600;">${escapeHTMLText(name)}'s other attributes (${lib.others.length})</summary>
      <div class="table-wrapper"><table>
        <thead><tr><th>Code</th><th>Attribute</th><th>Used for</th><th>Status</th></tr></thead>
        <tbody>${lib.others.map(c => `<tr><td>${escapeHTMLText(c.code)}</td><td>${escapeHTMLText(c.label)}</td><td>${escapeHTMLText(c.used_for || 'Both')}</td><td>${escapeHTMLText(c.status)}</td></tr>`).join('')}</tbody>
      </table></div>
    </details>` : ''}
    <div class="table-panel" style="position:sticky; bottom:0; z-index:2;">
      <div style="padding:14px 16px; display:flex; justify-content:space-between; align-items:center; gap:12px;">
        <span data-al-count></span>
        <button type="button" class="btn btn-primary" data-al-apply>Add to ${escapeHTMLText(name)}</button>
      </div>
    </div>`;

  const picks = () => [...root.querySelectorAll('[data-al-pick]:checked:not(:disabled)')].map(box => ({
    id: box.getAttribute('data-al-pick'),
    used_for: root.querySelector(`[data-al-use="${CSS.escape(box.getAttribute('data-al-pick'))}"]`)?.value || ''
  }));
  const count = () => {
    const p = picks();
    const v = p.filter(x => x.used_for === USE_VARIANT).length;
    root.querySelector('[data-al-count]').textContent = p.length
      ? `${p.length} ticked: ${v} variant, ${p.length - v} product information.`
      : 'Nothing ticked.';
    root.querySelector('[data-al-apply]').disabled = p.length === 0;
  };
  root.addEventListener('change', e => { if (e.target.matches('[data-al-pick], [data-al-use]')) count(); });
  root.querySelectorAll('[data-al-all], [data-al-none]').forEach(btn => btn.addEventListener('click', () => {
    const id = btn.getAttribute('data-al-all') || btn.getAttribute('data-al-none');
    const on = btn.hasAttribute('data-al-all');
    root.querySelectorAll(`[data-al-section="${id}"] [data-al-pick]:not(:disabled)`).forEach(box => { box.checked = on; });
    count();
  }));
  root.querySelector('[data-al-back]').addEventListener('click', () => renderView('control-panel'));
  root.querySelector('[data-al-industry]').addEventListener('change', e => {
    container.innerHTML = '';
    renderAttributeLibrary(container, client, e.target.value);
  });
  root.querySelector('[data-al-apply]').addEventListener('click', async e => {
    const btn = e.currentTarget; // null once the handler awaits
    const p = picks();
    if (!p.length) return;
    const v = p.filter(x => x.used_for === USE_VARIANT).length;
    if (!await showCustomConfirm(`Add ${p.length} attribute(s) to ${name} from the ${industryName(lib.industry)} library - ${v} as variant attributes, ${p.length - v} as product information?\n\n${name} can switch any of them off and remove values, but cannot delete them. Nothing it already has is changed.`, 'Add attributes')) return;
    btn.disabled = true;
    const r = await apiFetch(url, { method: 'POST', body: JSON.stringify({ attributes: p }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, `Could not add the attributes to ${name}.`); container.innerHTML = ''; renderAttributeLibrary(container, client, lib.industry); return; }
    const out = await r.json();
    await showCustomAlert(`Added ${(out.added || []).length} attribute(s) to ${name}.${(out.skipped || []).length ? ` ${(out.skipped || []).length} were already there and were left as they are.` : ''}`, 'Attributes added');
    container.innerHTML = '';
    renderAttributeLibrary(container, client, lib.industry);
  });
  count();
}
