// Stage 47.13.4 / 146.5.9 - PIM: attribute stewardship register and image order per channel.
// Lazy: loaded by view-pim.js for the 'stewardship' page and from the Media Library page.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
await loadViewStyles('view-pim-data.css');

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const opts = (rows, blank, sel) => (blank ? `<option value="">${esc(blank)}</option>` : '') +
  rows.map(r => `<option value="${esc(r.id)}"${r.id === sel ? ' selected' : ''}>${esc(r.name || r.id)}</option>`).join('');
const strs = (list, blank) => opts(list.map(v => ({ id: v })), blank);

function panel(html) {
  const el = document.createElement('div');
  el.className = 'table-panel';
  el.innerHTML = html;
  return el;
}

// ---- Stewardship register --------------------------------------------------
export async function renderStewardship(container) {
  const r = await apiFetch('/api/v1/pim/stewardship');
  if (!r) return;
  if (!r.ok) {
    container.innerHTML = '';
    container.appendChild(panel('<p class="text-muted">The register was not loaded.</p>'));
    await showApiError(r, 'The register was not loaded.');
    return;
  }
  const j = await r.json();
  const rows = j.attributes || [];
  const lookups = { users: (j.lookups || {}).users || [], channels: (j.lookups || {}).channels || [] };
  const canChange = canCreateDoctype('StewardshipChange');
  const userName = (id) => (lookups.users.find(u => u.id === id) || {}).name || id || '';
  const attrs = rows.map(a => ({ id: a.attribute, name: a.label }));
  const byAttr = (id) => rows.find(a => a.attribute === id) || {};

  const list = panel(`<div class="table-wrapper"><table><thead><tr><th>Attribute</th><th>System of record</th><th>Owner</th><th>Steward</th>
    <th>Confidence</th><th>Survivorship</th><th>Complete</th><th></th></tr></thead><tbody>${rows.length ? rows.map(a => {
      const e = a.entry;
      const sor = e ? e.system_of_record + (e.sor_ref ? ` ${e.sor_ref}` : '') : '';
      const done = a.required ? `${a.filled} of ${a.required}` : '';
      return `<tr><td>${esc(a.label)}</td><td>${esc(sor)}</td><td>${esc(e ? userName(e.data_owner) : '')}</td><td>${esc(e ? userName(e.data_steward) : '')}</td>
        <td>${esc(e ? e.confidence : '')}</td><td>${esc(e ? e.survivorship : '')}</td><td>${esc(done)}</td>
        <td><button type="button" class="btn btn-outline btn-sm" data-stw-impact="${esc(a.attribute)}">Impact</button>
        ${canChange && !a.pending ? `<button type="button" class="btn btn-outline btn-sm" data-stw-edit="${esc(a.attribute)}">${e ? 'Change' : 'Set'}</button>` : ''}
        ${canChange && e && !a.pending ? `<button type="button" class="btn btn-outline btn-sm" data-stw-remove="${esc(a.attribute)}">Remove</button>` : ''}
        ${a.pending ? `<span class="text-muted">Waiting for approval</span>` : ''}
        ${a.apply_error ? `<span class="badge badge-danger">Not applied</span> <span class="text-muted">${esc(a.apply_error)}</span>` : ''}</td></tr>`;
    }).join('') : '<tr><td colspan="8" class="text-muted">No attributes.</td></tr>'}</tbody></table></div>`);
  const detail = panel('');
  detail.classList.add('pd-panel', 'hidden');
  const listPage = document.createElement('div');
  listPage.append(list, detail);

  let section = null;
  const sendChange = async (body, errEl, btn) => {
    btn.disabled = true;
    try {
      const saved = await apiFetch('/api/v1/doc/StewardshipChange', { method: 'POST', body: JSON.stringify({ ...body, status: 'Draft' }) });
      if (!saved) return false;
      if (!saved.ok) {
        const d = await saved.json().catch(() => ({}));
        if (errEl) { errEl.textContent = d.error || d.message || 'The request was not saved.'; errEl.classList.remove('hidden'); }
        return false;
      }
      const doc = await saved.json();
      const s = await apiFetch('/api/v1/approval/submit', { method: 'POST', body: JSON.stringify({ doctype: 'StewardshipChange', document_id: doc.id }) });
      if (!s) return false;
      if (!s.ok) { await showApiError(s, 'The request was saved but not sent.'); return false; }
      showToast('Sent for approval.', { variant: 'info' });
      return true;
    } finally {
      btn.disabled = false;
    }
  };

  let form = null;
  if (canChange) {
    form = panel(`<div class="pd-form">
      <label>Attribute<select class="form-select" id="stw-attr">${opts(attrs, 'Choose')}</select></label>
      <label>System of record<select class="form-select" id="stw-sor">${strs(['ERP', 'Import', 'Channel', 'Person'], 'Choose')}</select></label>
      <label id="stw-ref-ch" class="hidden">Channel<select class="form-select" id="stw-ref-c">${strs(lookups.channels, 'Choose')}</select></label>
      <label id="stw-ref-pe" class="hidden">Person<select class="form-select" id="stw-ref-p">${opts(lookups.users, 'Choose')}</select></label>
      <label>Owner<select class="form-select" id="stw-owner">${opts(lookups.users, 'Choose')}</select></label>
      <label>Steward<select class="form-select" id="stw-steward">${opts(lookups.users, 'Choose')}</select></label>
      <label>Confidence<select class="form-select" id="stw-conf">${strs(['High', 'Medium', 'Low'], 'Choose')}</select></label>
      <label>Effective from<input type="date" class="form-input" id="stw-eff"></label>
      <label>Survivorship<select class="form-select" id="stw-surv">${strs(['Latest write wins', 'System of record wins'], 'Choose')}</select></label>
      <label>Markets<input class="form-input" id="stw-mkt"></label>
      <label>Channels<input class="form-input" id="stw-chs"></label>
      <label>Languages<input class="form-input" id="stw-lng"></label>
      <div class="pd-bar"><button type="button" class="btn btn-primary" id="stw-send">Send for approval</button></div>
      <p class="text-danger hidden" id="stw-err"></p></div>`);
    const f = (id) => form.querySelector(id);
    const showRef = () => {
      f('#stw-ref-ch').classList.toggle('hidden', f('#stw-sor').value !== 'Channel');
      f('#stw-ref-pe').classList.toggle('hidden', f('#stw-sor').value !== 'Person');
    };
    f('#stw-sor').addEventListener('change', showRef);
    f('#stw-send').addEventListener('click', async (e) => {
      const sor = f('#stw-sor').value;
      const body = { action: 'Set', attribute: f('#stw-attr').value, system_of_record: sor,
        sor_ref: sor === 'Channel' ? f('#stw-ref-c').value : sor === 'Person' ? f('#stw-ref-p').value : '',
        data_owner: f('#stw-owner').value, data_steward: f('#stw-steward').value, confidence: f('#stw-conf').value,
        effective_from: f('#stw-eff').value, survivorship: f('#stw-surv').value,
        markets: f('#stw-mkt').value.trim(), channels: f('#stw-chs').value.trim(), languages: f('#stw-lng').value.trim() };
      if (await sendChange(body, f('#stw-err'), e.currentTarget)) { f('#stw-err').classList.add('hidden'); txnDone('pim-stw'); renderView('pim:stewardship'); }
    });
    form.fillFrom = (id) => {
      const a = byAttr(id), x = a.entry || {};
      f('#stw-attr').value = id;
      f('#stw-sor').value = x.system_of_record || '';
      showRef();
      f('#stw-ref-c').value = x.system_of_record === 'Channel' ? x.sor_ref || '' : '';
      f('#stw-ref-p').value = x.system_of_record === 'Person' ? x.sor_ref || '' : '';
      f('#stw-owner').value = x.data_owner || '';
      f('#stw-steward').value = x.data_steward || '';
      f('#stw-conf').value = x.confidence || '';
      f('#stw-eff').value = x.effective_from || '';
      f('#stw-surv').value = x.survivorship || '';
      f('#stw-mkt').value = x.markets || '';
      f('#stw-chs').value = x.channels || '';
      f('#stw-lng').value = x.languages || '';
    };
  }

  if (form) {
    section = txnPagedSection({ id: 'pim-stw', title: 'Stewardship', page: 'New entry', count: rows.length, newLabel: 'New Entry', form, list: listPage, heading: 'h2' });
    container.appendChild(section);
    txnRestorePane('pim-stw');
  } else {
    const h = document.createElement('h2');
    h.textContent = 'Stewardship';
    container.appendChild(h);
    container.appendChild(listPage);
  }

  list.addEventListener('click', async (ev) => {
    const b = ev.target.closest('button');
    if (!b) return;
    if (b.dataset.stwImpact) {
      const id = b.dataset.stwImpact;
      const d = await apiFetch(`/api/v1/pim/stewardship/${encodeURIComponent(id)}`);
      if (!d) return;
      if (!d.ok) { await showApiError(d, 'The impact was not loaded.'); return; }
      const x = await d.json();
      detail.classList.remove('hidden');
      detail.innerHTML = `<h3 class="pd-sub">${esc(byAttr(id).label || id)}</h3>
        <div class="table-wrapper"><table><thead><tr><th>Used by</th><th>Name</th><th>Detail</th><th>Items</th></tr></thead><tbody>${
          (x.consumers || []).map(c => `<tr><td>${esc(c.kind)}</td><td>${esc(c.name)}</td><td>${esc(c.detail)}</td><td>${c.items}</td></tr>`).join('')}</tbody></table></div>
        <h3 class="pd-sub">Latest values</h3>
        ${(x.writes || []).length ? `<div class="table-wrapper"><table><thead><tr><th>Item</th><th>Value</th><th>Source</th><th>Confidence</th><th>By</th><th>At</th></tr></thead><tbody>${
          (x.writes || []).map(w => `<tr><td>${esc(w.item)}</td><td>${esc(w.value)}</td><td>${esc(w.source + (w.source_ref ? ' ' + w.source_ref : ''))}</td><td>${esc(w.confidence)}</td><td>${esc(userName(w.by))}</td><td>${esc(w.at)}</td></tr>`).join('')}</tbody></table></div>` : '<p class="text-muted">No values.</p>'}`;
    } else if (b.dataset.stwEdit && form) {
      form.fillFrom(b.dataset.stwEdit);
      window.txnShowPane('pim-stw:new');
    } else if (b.dataset.stwRemove) {
      if (!(await showCustomConfirm(`Remove the stewardship entry for ${byAttr(b.dataset.stwRemove).label || b.dataset.stwRemove}?`, 'Remove entry'))) return;
      if (await sendChange({ action: 'Remove', attribute: b.dataset.stwRemove }, null, b)) renderView('pim:stewardship');
    }
  });
}

// ---- Image order per channel (146.5.9) -------------------------------------
export async function mountChannelOrder(host) {
  const cr = await apiFetch('/api/v1/doc/Channel?limit=200');
  const channels = cr && cr.ok ? await cr.json() : [];
  const box = panel(`<h3 class="pd-sub">Image order per channel</h3><div class="pd-bar">
    <input class="form-input" id="mo-item" placeholder="Item code">
    <select class="form-select" id="mo-ch">${opts(channels.map(c => ({ id: c.id, name: c.name || c.id })), 'Channel')}</select>
    <button type="button" class="btn btn-outline" id="mo-show">Show</button>
    <button type="button" class="btn btn-primary hidden" id="mo-save">Save order</button>
    <button type="button" class="btn btn-outline hidden" id="mo-reset">Reset</button></div><div id="mo-list"></div>`);
  box.classList.add('pd-panel');
  host.appendChild(box);
  const q = (id) => box.querySelector(id);
  let ids = [];
  let meta = {};
  const draw = () => {
    q('#mo-list').innerHTML = ids.length ? `<div class="table-wrapper"><table><tbody>${ids.map((id, i) => `<tr><td>${i + 1}</td><td>${esc(id)}</td>
      <td>${esc((meta[id] || {}).media_role)}</td><td>${esc((meta[id] || {}).file_type)}</td>
      <td><button type="button" class="btn btn-outline btn-sm" data-mo-up="${i}"${i === 0 ? ' disabled' : ''}>Up</button>
      <button type="button" class="btn btn-outline btn-sm" data-mo-down="${i}"${i === ids.length - 1 ? ' disabled' : ''}>Down</button></td></tr>`).join('')}</tbody></table></div>`
      : '<p class="text-muted">No images.</p>';
    q('#mo-save').classList.toggle('hidden', !ids.length);
    q('#mo-reset').classList.toggle('hidden', !ids.length);
  };
  const qs = () => `item=${encodeURIComponent(q('#mo-item').value.trim())}&channel=${encodeURIComponent(q('#mo-ch').value)}`;
  q('#mo-show').addEventListener('click', async () => {
    const r = await apiFetch(`/api/v1/pim/media-order?${qs()}`);
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The images were not loaded.'); return; }
    const d = await r.json();
    d.images = d.images || [];
    meta = Object.fromEntries(d.images.map(m => [m.id, m]));
    ids = d.images.map(m => m.id);
    draw();
  });
  q('#mo-list').addEventListener('click', (ev) => {
    const b = ev.target.closest('button');
    if (!b) return;
    const up = b.dataset.moUp !== undefined, i = Number(up ? b.dataset.moUp : b.dataset.moDown), j = up ? i - 1 : i + 1;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    draw();
  });
  const post = async (list, done) => {
    const r = await apiFetch('/api/v1/pim/media-order', { method: 'POST', body: JSON.stringify({ item: q('#mo-item').value.trim(), channel: q('#mo-ch').value, ids: list }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The order was not saved.'); return; }
    showToast(done, { variant: 'info' });
    q('#mo-show').click();
  };
  q('#mo-save').addEventListener('click', () => post(ids, 'Order saved.'));
  q('#mo-reset').addEventListener('click', () => post([], 'Order cleared.'));
}
