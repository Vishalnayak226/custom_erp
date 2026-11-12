// Stages 148.21 / 148.24: journal templates, copy last month, FX revaluation and
// intercompany entry. Loaded on demand from the Journal Vouchers screen
// (view-journal.js); shared services (apiFetch, showApiError, showToast,
// showCustomConfirm, guardAgainstDoubleSubmit, attachLinkTypeahead,
// escapeHTMLText, localISODate) are globals. No posting logic lives here: each
// button calls a route that already exists and keeps its own role check.

function jtDialog(id, title, fields, saveLabel, onSave, extraHTML = '') {
  document.getElementById(id)?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = id;
  const rows = fields.map(f => `<div class="form-group"><label class="form-label" for="${id}-${f.name}">${escapeHTMLText(f.label)}</label>` +
    (f.options
      ? `<select id="${id}-${f.name}" class="form-select">${f.options.map(o => `<option value="${escapeHTMLText(o.value)}">${escapeHTMLText(o.label)}</option>`).join('')}</select></div>`
      : `<input id="${id}-${f.name}" class="form-input${f.type === 'number' ? ' num' : ''}" type="${f.type || 'text'}" ${f.type === 'number' ? 'min="0" step="1"' : ''} autocomplete="off" value="${escapeHTMLText(f.value || '')}"></div>`)).join('');
  overlay.innerHTML = `
    <div class="modal-container s-acb48b" role="dialog" aria-modal="true" aria-labelledby="${id}-title">
      <div class="modal-header"><h3 class="modal-title" id="${id}-title">${escapeHTMLText(title)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-jt-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body s-63bcac">${rows}${extraHTML}</div>
      <div class="modal-footer"><button type="button" class="btn btn-outline" data-jt-close>Cancel</button>
        ${saveLabel ? `<button type="button" class="btn btn-primary" data-jt-save>${escapeHTMLText(saveLabel)}</button>` : ''}</div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelectorAll('[data-jt-close]').forEach(b => b.addEventListener('click', close));
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const get = name => overlay.querySelector(`#${id}-${name}`).value.trim();
  const save = overlay.querySelector('[data-jt-save]');
  if (save) save.addEventListener('click', () => guardAgainstDoubleSubmit(save, 'Saving...', async () => {
    if (await onSave(get, overlay)) close();
  }));
  return overlay;
}

// ---- 148.21 templates ----------------------------------------------------

const JT_LINE_KEYS = ['cost_center', 'department', 'store', 'employee', 'tag', 'narration'];

// A voucher line as the editor holds it (strings), from a stored line.
function jtEditorLine(l) {
  const out = { account: l.account_code || '', debit: l.debit ? String(l.debit) : '', credit: l.credit ? String(l.credit) : '' };
  JT_LINE_KEYS.forEach(k => { out[k] = l[k] || ''; });
  return out;
}

// Whole rupees split by percent; the last line on a side takes the rounding.
function jtSplit(lines, total) {
  const out = lines.map(l => ({ ...l }));
  ['debit', 'credit'].forEach(side => {
    const idx = out.map((l, i) => (Number(l[side + '_pct']) > 0 ? i : -1)).filter(i => i >= 0);
    let left = total;
    idx.forEach((i, n) => {
      const v = n === idx.length - 1 ? left : Math.round(total * Number(out[i][side + '_pct']) / 100);
      out[i][side] = v;
      left -= v;
    });
  });
  return out;
}

async function jtTemplates() {
  const res = await apiFetch('/api/v1/doc/JournalTemplate?limit=500');
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'The templates could not be loaded.'); return null; }
  return (await res.json()).filter(t => t.status !== 'Inactive');
}

async function openUseJournalTemplate(apply) {
  const list = await jtTemplates();
  if (!list) return;
  if (!list.length) { showToast('No templates yet.', { variant: 'error' }); return; }
  const overlay = jtDialog('jt-use', 'Use template', [
    { name: 'tpl', label: 'Template', options: list.map(t => ({ value: t.id, label: t.name || t.id })) },
    { name: 'amount', label: 'Amount', type: 'number' },
  ], 'Use', async get => {
    const t = list.find(x => x.id === get('tpl'));
    let lines;
    try { lines = JSON.parse(t.lines || '[]'); } catch { lines = []; }
    if (t.split_mode === 'Percent') {
      const amount = parseInt(get('amount'), 10) || 0;
      if (amount <= 0) { showToast('Enter the amount.', { variant: 'error' }); return false; }
      lines = jtSplit(lines, amount);
    }
    apply(lines.map(jtEditorLine), t.narration || '');
    return true;
  });
  const sync = () => {
    const t = list.find(x => x.id === overlay.querySelector('#jt-use-tpl').value);
    overlay.querySelector('#jt-use-amount').closest('.form-group').style.display = t && t.split_mode === 'Percent' ? '' : 'none';
  };
  overlay.querySelector('#jt-use-tpl').addEventListener('change', sync);
  sync();
}

function openSaveJournalTemplate(lines, narration) {
  const used = lines.filter(l => l.account || l.debit || l.credit);
  if (used.length < 2) { showToast('Fill the lines first.', { variant: 'error' }); return; }
  jtDialog('jt-save', 'Save as template', [
    { name: 'name', label: 'Name' },
    { name: 'mode', label: 'Split', options: [{ value: 'Amount', label: 'Amounts' }, { value: 'Percent', label: 'Percent of an amount' }] },
  ], 'Save', async get => {
    if (!get('name')) { showToast('Enter a name.', { variant: 'error' }); return false; }
    const percent = get('mode') === 'Percent';
    const sum = f => used.reduce((s, l) => s + (parseInt(l[f], 10) || 0), 0);
    const totals = { debit: sum('debit'), credit: sum('credit') };
    const seen = { debit: 0, credit: 0 };
    const last = { debit: -1, credit: -1 };
    used.forEach((l, i) => { ['debit', 'credit'].forEach(s => { if (parseInt(l[s], 10) > 0) last[s] = i; }); });
    const out = used.map((l, i) => {
      const row = { account_code: l.account };
      JT_LINE_KEYS.forEach(k => { if (l[k]) row[k] = l[k]; });
      ['debit', 'credit'].forEach(s => {
        const v = parseInt(l[s], 10) || 0;
        if (v <= 0) return;
        if (!percent) { row[s] = v; return; }
        // Hundredths of a percent, the last line takes the rest so each side is exactly 100.
        const p = i === last[s] ? 10000 - seen[s] : Math.round(v * 10000 / totals[s]);
        seen[s] += p;
        row[s + '_pct'] = p / 100;
      });
      return row;
    });
    const res = await apiFetch('/api/v1/doc/JournalTemplate', { method: 'POST', body: JSON.stringify({ name: get('name'), narration, split_mode: get('mode'), lines: JSON.stringify(out), status: 'Active' }) });
    if (!res) return false;
    if (!res.ok) { await showApiError(res, 'The template was not saved.'); return false; }
    showToast('Template saved.', { variant: 'success' });
    return true;
  });
}

function jtSameDayNextMonth(iso) {
  const [y, m, d] = iso.split('-').map(Number);
  const last = new Date(y, m + 1, 0).getDate();
  return localISODate(new Date(y, m, Math.min(d, last)));
}

// Last calendar month's vouchers (not reversals, not rejected) to copy from.
function openCopyLastMonth(vouchers, apply) {
  const now = new Date();
  const start = localISODate(new Date(now.getFullYear(), now.getMonth() - 1, 1));
  const end = localISODate(new Date(now.getFullYear(), now.getMonth(), 0));
  const rows = vouchers.filter(v => v.voucher_date >= start && v.voucher_date <= end && !v.reversed_from &&
    ['Draft', 'Pending Approval', 'Approved', 'Posted', 'Reversed'].includes(v.status));
  if (!rows.length) { showToast('No vouchers last month.', { variant: 'error' }); return; }
  jtDialog('jt-copy', 'Copy last month', [
    { name: 'v', label: 'Voucher', options: rows.map(v => ({ value: v.id, label: `${v.voucher_date} · ${v.narration || v.voucher_number || ''} · ${formatNumber(v.total_amount || 0)}` })) },
  ], 'Copy', async get => {
    const v = rows.find(x => x.id === get('v'));
    let lines;
    try { lines = typeof v.lines === 'string' ? JSON.parse(v.lines) : (v.lines || []); } catch { lines = []; }
    apply(lines.map(jtEditorLine), v.narration || '', jtSameDayNextMonth(v.voucher_date));
    return true;
  });
}

// ---- 148.24 FX revaluation and intercompany ------------------------------

const jtMoney = n => formatNumber(n || 0);

function openFxRevaluation() {
  const overlay = jtDialog('jt-fx', 'FX revaluation', [
    { name: 'asof', label: 'As of', type: 'date', value: localISODate(new Date()) },
  ], '', async () => true, '<div id="jt-fx-out"></div><div><button type="button" class="btn btn-outline" id="jt-fx-preview">Preview</button> <button type="button" class="btn btn-primary hidden" id="jt-fx-post">Post</button></div>');
  const out = overlay.querySelector('#jt-fx-out');
  const asOf = () => overlay.querySelector('#jt-fx-asof').value;
  const post = overlay.querySelector('#jt-fx-post');
  overlay.querySelector('#jt-fx-preview').addEventListener('click', async () => {
    post.classList.add('hidden');
    const res = await apiFetch(`/api/v1/finance/fx-revaluation?as_of=${encodeURIComponent(asOf())}`);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The preview could not be made.'); return; }
    const run = await res.json();
    const lines = run.lines || [];
    out.innerHTML = lines.length === 0
      ? '<p class="s-d59bb5">Nothing to revalue.</p>'
      : `<div class="table-panel s-2509d2"><table><thead><tr><th>Document</th><th>Currency</th><th class="num">Booked</th><th class="num">Now</th><th class="num">Change</th></tr></thead><tbody>${lines.map(l =>
        `<tr><td>${escapeHTMLText(l.document_id)}</td><td>${escapeHTMLText(l.currency)}</td><td class="num">${jtMoney(l.carrying_amount)}</td><td class="num">${jtMoney(l.revalued_amount)}</td><td class="num">${jtMoney(l.adjustment)}</td></tr>`).join('')}</tbody></table></div>
        <div>Gain <b>${jtMoney(run.net_gain)}</b> · Loss <b>${jtMoney(run.net_loss)}</b></div>`;
    if (lines.some(l => l.adjustment)) post.classList.remove('hidden');
  });
  post.addEventListener('click', () => guardAgainstDoubleSubmit(post, 'Posting...', async () => {
    if (!await showCustomConfirm('Post the revaluation to the ledger?', 'FX revaluation')) return;
    const res = await apiFetch('/api/v1/finance/fx-revaluation', { method: 'POST', body: JSON.stringify({ as_of: asOf() }) });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'The revaluation was not posted.'); return; }
    const run = await res.json();
    showToast(`${run.documents_posted || 0} posted.`, { variant: 'success' });
    overlay.remove();
  }));
}

function openIntercompanyEntry(onDone) {
  const overlay = jtDialog('jt-ic', 'Intercompany entry', [
    { name: 'date', label: 'Date', type: 'date', value: localISODate(new Date()) },
    { name: 'narration', label: 'Narration' },
    { name: 'fromEntity', label: 'From entity' }, { name: 'fromAcct', label: 'From account' },
    { name: 'toEntity', label: 'To entity' }, { name: 'toAcct', label: 'To account' },
    { name: 'amount', label: 'Amount', type: 'number' },
  ], 'Save as Draft', async get => {
    const res = await apiFetch('/api/v1/finance/intercompany-transaction', {
      method: 'POST',
      body: JSON.stringify({ transaction_date: get('date'), narration: get('narration'), from_entity: get('fromEntity'), from_account_code: get('fromAcct'),
        to_entity: get('toEntity'), to_account_code: get('toAcct'), amount: Number(get('amount')) || 0 })
    });
    if (!res) return false;
    if (!res.ok) { await showApiError(res, 'The entry was not saved.'); return false; }
    const made = await res.json();
    showToast('Saved as Draft.', { variant: 'success' });
    if (await showCustomConfirm('Send it for approval now?', 'Intercompany entry')) {
      const sub = await apiFetch('/api/v1/approval/submit', { method: 'POST', body: JSON.stringify({ doctype: 'IntercompanyTransaction', document_id: made.id }) });
      if (sub && !sub.ok) await showApiError(sub, 'It was not sent for approval.');
      else if (sub) showToast('Sent for approval.', { variant: 'success' });
    }
    if (onDone) onDone();
    return true;
  });
  attachLinkTypeahead(overlay.querySelector('#jt-ic-fromEntity'), 'LegalEntity', { noSetupHint: true });
  attachLinkTypeahead(overlay.querySelector('#jt-ic-toEntity'), 'LegalEntity', { noSetupHint: true });
}

export { openUseJournalTemplate, openSaveJournalTemplate, openCopyLastMonth, openFxRevaluation, openIntercompanyEntry };
