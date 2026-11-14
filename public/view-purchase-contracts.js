// Blanket / rate contracts: list first, New is its own page, an open contract
// shows what has been released against it. Release PO opens the New PO page.
const { txnPagedSection, txnRestorePane, txnDone, txnMoney } = await import('./txn-screen.js' + new URL(import.meta.url).search);

const SCREEN = 'purchase-contracts-screen';

async function renderPurchaseContractsView(container) {
  const res = await apiFetch('/api/v1/doc/PurchaseContract');
  if (!res) return;
  const rows = res.ok ? await res.json() : [];

  const form = document.createElement('div');
  form.className = 'table-panel pc-form';
  form.innerHTML = `<div class="pc-head">
      <div class="form-group"><label class="form-label" for="pc-vendor">Vendor</label><input id="pc-vendor" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="pc-kind">Kind</label><select id="pc-kind" class="form-input"><option>Blanket</option><option>Rate contract</option></select></div>
      <div class="form-group"><label class="form-label" for="pc-from">Valid from</label><input id="pc-from" type="date" class="form-input"></div>
      <div class="form-group"><label class="form-label" for="pc-to">Valid to</label><input id="pc-to" type="date" class="form-input"></div>
      <div class="form-group"><label class="form-label" for="pc-store">Store</label><input id="pc-store" class="form-input" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="pc-cap">Cap value</label><input id="pc-cap" type="number" min="0" step="any" class="form-input num"></div>
    </div>
    <div class="table-wrapper"><table><thead><tr><th>Item</th><th class="num">Rate</th><th class="num">Cap qty</th><th></th></tr></thead><tbody id="pc-lines"></tbody></table></div>
    <div class="pc-actions"><button class="btn btn-outline" id="pc-add" type="button">Add item</button><button class="btn btn-primary" id="pc-save" type="button">Save</button></div>`;

  const detail = document.createElement('div');
  detail.className = 'table-panel pc-detail';

  const badge = s => `<span class="badge ${s === 'Active' ? 'badge-success' : s === 'Draft' ? 'badge-warning' : 'badge-neutral'}">${escapeHTMLText(s)}</span>`;
  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `<table><thead><tr><th>Contract</th><th>Vendor</th><th>Kind</th><th>Valid</th><th class="num">Cap</th><th>Status</th></tr></thead><tbody>${rows.length === 0
    ? '<tr><td colspan="6" class="text-muted">No contracts yet.</td></tr>'
    : rows.map(r => `<tr class="pc-row" data-pc-open="${escapeHTMLText(r.id)}"><td>${escapeHTMLText(r.code || r.id)}</td><td>${escapeHTMLText(r.vendor || '')}</td>
      <td>${escapeHTMLText(r.contract_kind || '')}</td><td>${escapeHTMLText(r.valid_from || '')} - ${escapeHTMLText(r.valid_to || '')}</td>
      <td class="num">${r.cap_value ? txnMoney(r.cap_value) : ''}</td><td>${badge(r.status || '')}</td></tr>`).join('')}</tbody></table>`;

  const section = txnPagedSection({ id: SCREEN, title: 'Contracts', page: 'New contract', count: rows.length, newLabel: 'New contract', form, list });
  const detailPane = document.createElement('div');
  detailPane.dataset.pane = 'open';
  detailPane.className = 'hidden';
  detailPane.appendChild(detail);
  section.appendChild(detailPane);
  container.appendChild(section);
  txnRestorePane(SCREEN);

  attachLinkTypeahead(document.getElementById('pc-vendor'), 'Vendor');
  attachLinkTypeahead(document.getElementById('pc-store'), 'Location');
  const body = document.getElementById('pc-lines');
  const addLine = () => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td><input class="form-input pc-sku" autocomplete="off"></td><td><input type="number" min="0" step="any" class="form-input num pc-rate"></td>
      <td><input type="number" min="0" step="any" class="form-input num pc-capqty"></td><td><button type="button" class="action-btn action-btn-danger">Remove</button></td>`;
    tr.querySelector('button').addEventListener('click', () => tr.remove());
    body.appendChild(tr);
    attachLinkTypeahead(tr.querySelector('.pc-sku'), 'Item');
  };
  addLine();
  document.getElementById('pc-add').addEventListener('click', addLine);

  document.getElementById('pc-save').addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Saving...', async () => {
    const lines = [...body.querySelectorAll('tr')].map(tr => {
      const l = { sku: tr.querySelector('.pc-sku').value.trim(), rate: Number(tr.querySelector('.pc-rate').value) || 0 };
      const q = Number(tr.querySelector('.pc-capqty').value);
      if (q > 0) l.cap_qty = q;
      return l;
    }).filter(l => l.sku);
    const val = id => document.getElementById(id).value.trim();
    if (!val('pc-vendor') || !val('pc-from') || !val('pc-to') || lines.length === 0) { showToast('Enter the vendor, the dates and at least one item.'); return; }
    const data = { vendor: val('pc-vendor'), contract_kind: val('pc-kind'), valid_from: val('pc-from'), valid_to: val('pc-to'), lines: JSON.stringify(lines), status: 'Draft' };
    if (val('pc-store')) data.location = val('pc-store');
    if (Number(val('pc-cap')) > 0) data.cap_value = Number(val('pc-cap'));
    const r = await apiFetch('/api/v1/doc/PurchaseContract', { method: 'POST', body: JSON.stringify(data) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The contract was not saved.'); return; }
    txnDone(SCREEN);
    renderView('purchase-contracts');
  }));

  const pageTitle = text => { const t = section.querySelector('.txn-page-only .txn-title'); if (t) t.textContent = text; };
  section.querySelectorAll('.page-section-actions .btn').forEach(b => b.addEventListener('click', () => pageTitle('New contract')));

  async function openContract(id) {
    const r = await apiFetch(`/api/v1/procurement/contract/${encodeURIComponent(id)}/summary`);
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The contract could not be read.'); return; }
    const s = await r.json();
    const act = (key, label, cls) => `<button type="button" class="btn ${cls}" data-pc-act="${key}">${label}</button>`;
    detail.innerHTML = `<div class="pc-detail-head"><strong>${escapeHTMLText(s.id)}</strong> ${badge(s.status)}
        <span class="text-muted">${escapeHTMLText(s.vendor)} · ${escapeHTMLText(s.kind)} · ${escapeHTMLText(s.valid_from)} - ${escapeHTMLText(s.valid_to)}${s.location ? ' · ' + escapeHTMLText(s.location) : ''}</span>
        <span class="pc-detail-actions">${s.status === 'Draft' ? act('activate', 'Activate', 'btn-primary') : ''}${s.status === 'Active' ? act('release', 'Release PO', 'btn-primary') : ''}${s.status === 'Active' || s.status === 'Expired' ? act('close', 'Close', 'btn-outline') : ''}</span></div>
      <p class="pc-cap">${s.cap_value ? `Released ${txnMoney(s.consumed_value)} of ${txnMoney(s.cap_value)} · ${txnMoney(s.remaining_value)} left` : `Released ${txnMoney(s.consumed_value)} · no cap`}</p>
      <table><thead><tr><th>Item</th><th class="num">Rate</th><th class="num">Cap qty</th><th class="num">Released qty</th></tr></thead><tbody>${(s.lines || []).map(l =>
        `<tr><td>${escapeHTMLText(l.sku)}</td><td class="num">${txnMoney(l.rate)}</td><td class="num">${l.cap_qty || ''}</td><td class="num">${l.consumed_qty || 0}</td></tr>`).join('')}</tbody></table>
      <h3 class="pc-sub">Release POs</h3>
      <table><thead><tr><th>PO</th><th>Date</th><th>Status</th><th class="num">Value</th></tr></thead><tbody>${(s.releases || []).length === 0
        ? '<tr><td colspan="4" class="text-muted">No releases yet.</td></tr>'
        : s.releases.map(p => `<tr><td>${escapeHTMLText(p.id)}</td><td>${escapeHTMLText(p.date || '')}</td><td>${escapeHTMLText(p.status)}</td><td class="num">${txnMoney(p.value)}</td></tr>`).join('')}</tbody></table>`;
    pageTitle(s.id);
    window.txnShowPane(`${SCREEN}:open`);
    detail.querySelectorAll('[data-pc-act]').forEach(b => b.addEventListener('click', e => guardAgainstDoubleSubmit(e.currentTarget, 'Working...', async () => {
      if (b.dataset.pcAct === 'release') { await import('./view-procurement.js' + new URL(import.meta.url).search); window.newPOFromContract(s.id, s.vendor, s.location || ''); return; }
      const rr = await apiFetch(`/api/v1/procurement/contract/${encodeURIComponent(s.id)}/${b.dataset.pcAct}`, { method: 'POST' });
      if (!rr) return;
      if (!rr.ok) { await showApiError(rr, 'The contract was not updated.'); return; }
      showToast(b.dataset.pcAct === 'activate' ? 'Contract activated.' : 'Contract closed.', { variant: 'success' });
      renderView('purchase-contracts');
    })));
  }
  list.querySelectorAll('[data-pc-open]').forEach(tr => tr.addEventListener('click', () => openContract(tr.dataset.pcOpen)));
}

export { renderPurchaseContractsView };
