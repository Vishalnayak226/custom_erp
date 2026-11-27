// Stage 148.5 - the Chart of Accounts list with Add / Change / Block from the app.
// Lazy: loaded by view-finance.js. Every change is a GLAccountChange request that
// goes through approval and is applied when approved.
const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
// 276.2: each user chooses the list's columns.
const { columnsButtonHTML, wireColumns } = await import('./list-columns.js' + new URL(import.meta.url).search);
await loadViewStyles('view-finance-coa.css');

const esc = (v) => escapeHTMLText(v == null ? '' : String(v));
const TYPES = ['Asset', 'Liability', 'Equity', 'Revenue', 'Expense'];

// Children sit under their parent, parents and loose accounts in code order.
function ordered(accounts) {
  const byParent = {};
  const codes = new Set(accounts.map(a => a.account_code));
  accounts.forEach(a => {
    const p = a.parent_code && codes.has(a.parent_code) ? a.parent_code : '';
    (byParent[p] = byParent[p] || []).push(a);
  });
  const out = [];
  const seen = new Set();
  const walk = (parent, depth) => (byParent[parent] || []).forEach(a => {
    if (seen.has(a.account_code)) return;
    seen.add(a.account_code);
    out.push({ a, depth });
    walk(a.account_code, depth + 1);
  });
  walk('', 0);
  accounts.forEach(a => { if (!seen.has(a.account_code)) out.push({ a, depth: 0 }); });
  return out;
}

export function renderChartOfAccounts(container, accounts) {
  const nameOf = (code) => { const a = accounts.find(x => x.account_code === code); return a ? a.account_name : ''; };
  const rows = ordered(accounts);
  const canChange = canCreateDoctype('GLAccountChange');
  const list = document.createElement('div');
  list.className = 'table-panel';
  const rowHtml = ({ a, depth }) => `<tr${a.blocked ? ' class="text-muted"' : ''}>
      <td class="s-355794">${esc(a.account_code)}</td>
      <td data-css="font-weight:600; padding-left:${12 + depth * 18}px;">${esc(a.account_name)}</td>
      <td>${esc(a.account_type)}</td>
      <td>${esc(nameOf(a.parent_code))}</td>
      <td>${[a.blocked ? 'Blocked' : '', a.no_manual_posting ? 'No manual posting' : ''].filter(Boolean).join(', ')}</td>
      ${canChange ? `<td><button type="button" class="btn btn-outline btn-sm" data-coa="${a.blocked ? 'Unblock' : 'Block'}" data-code="${esc(a.account_code)}">${a.blocked ? 'Unblock' : 'Block'}</button>
        <button type="button" class="btn btn-outline btn-sm" data-coa="Change" data-code="${esc(a.account_code)}">Change</button></td>` : ''}
    </tr>`;
  list.innerHTML = rows.length
    ? `<table><thead><tr><th>Account Code</th><th>Account Name</th><th>Type</th><th>Parent</th><th>Flags</th>${canChange ? '<th></th>' : ''}</tr></thead><tbody>${rows.map(rowHtml).join('')}</tbody></table>`
    : '<p class="text-muted s-287f77">No accounts yet.</p>';
  // 276.2: the walk found Parent and Flags blank on every row: off unless chosen.
  const columns = () => wireColumns(list.querySelector('table'), container.querySelector('#coa-columns-btn'), 'chart-of-accounts', ['Parent', 'Flags']);
  if (!canChange) {
    if (rows.length) list.insertAdjacentHTML('afterbegin', `<div class="table-controls">${columnsButtonHTML('coa-columns-btn')}</div>`);
    container.appendChild(list);
    columns();
    return;
  }

  const accountOptions = (blank) => `<option value="">${esc(blank)}</option>` +
    accounts.map(a => `<option value="${esc(a.account_code)}">${esc(a.account_name)}</option>`).join('');
  const form = document.createElement('div');
  form.className = 'table-panel';
  form.innerHTML = `<div class="coa-form">
    <label>Action<select class="form-select" id="coa-action"><option>Add</option><option>Change</option><option>Block</option><option>Unblock</option></select></label>
    <label id="coa-code-new">Code<input class="form-input" id="coa-code" maxlength="50"></label>
    <label id="coa-code-old">Account<select class="form-select" id="coa-pick">${accountOptions('Choose')}</select></label>
    <label>Name<input class="form-input" id="coa-name"></label>
    <label>Type<select class="form-select" id="coa-type"><option value="">As is</option>${TYPES.map(t => `<option>${t}</option>`).join('')}</select></label>
    <label>Parent<select class="form-select" id="coa-parent">${accountOptions('None')}</select></label>
    <label>No manual posting<select class="form-select" id="coa-flag"><option value="">As is</option><option>Yes</option><option>No</option></select></label>
    <label>Reason<input class="form-input" id="coa-reason"></label>
    <div><button type="button" class="btn btn-primary" id="coa-send">Send for approval</button></div>
    <div class="text-danger hidden" id="coa-err"></div></div>`;
  const section = txnPagedSection({ id: 'finance-coa', title: 'Chart of Accounts', page: 'Account request', count: accounts.length,
    newLabel: 'Account', form, list, heading: 'h2', extra: rows.length ? columnsButtonHTML('coa-columns-btn') : '' });
  container.appendChild(section);
  txnRestorePane('finance-coa');
  columns();

  const $ = (s) => form.querySelector(s);
  const sync = () => {
    const action = $('#coa-action').value;
    $('#coa-code-new').classList.toggle('hidden', action !== 'Add');
    $('#coa-code-old').classList.toggle('hidden', action === 'Add');
    ['#coa-name', '#coa-type', '#coa-parent', '#coa-flag'].forEach(s => $(s).closest('label').classList.toggle('hidden', action !== 'Add' && action !== 'Change'));
  };
  $('#coa-action').addEventListener('change', sync);
  sync();

  list.querySelectorAll('[data-coa]').forEach(btn => btn.addEventListener('click', () => {
    const a = accounts.find(x => x.account_code === btn.dataset.code);
    $('#coa-action').value = btn.dataset.coa;
    $('#coa-pick').value = a.account_code;
    $('#coa-name').value = a.account_name;
    $('#coa-type').value = '';
    $('#coa-parent').value = a.parent_code || '';
    $('#coa-flag').value = '';
    sync();
    const open = section.querySelector('button.txn-landing-only');
    if (open) open.click();
  }));

  $('#coa-send').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const err = $('#coa-err');
    const action = $('#coa-action').value;
    const body = { action, account_code: action === 'Add' ? $('#coa-code').value.trim() : $('#coa-pick').value,
      reason: $('#coa-reason').value.trim(), status: 'Draft' };
    if (action === 'Add' || action === 'Change') {
      Object.assign(body, { account_name: $('#coa-name').value.trim(), account_type: $('#coa-type').value,
        parent_code: $('#coa-parent').value, no_manual_posting: $('#coa-flag').value });
      if (action === 'Change' && !$('#coa-parent').value) {
        const a = accounts.find(x => x.account_code === body.account_code);
        if (a && a.parent_code) body.clear_parent = 'Yes';
      }
    }
    btn.disabled = true;
    try {
      const r = await apiFetch('/api/v1/doc/GLAccountChange', { method: 'POST', body: JSON.stringify(body) });
      if (!r) return;
      if (!r.ok) { const d = await r.json().catch(() => ({})); err.textContent = d.error || 'The request was not saved.'; err.classList.remove('hidden'); return; }
      const saved = await r.json();
      const s = await apiFetch('/api/v1/approval/submit', { method: 'POST', body: JSON.stringify({ doctype: 'GLAccountChange', document_id: saved.id }) });
      if (!s) return;
      if (!s.ok) { await showApiError(s, 'The request was saved but not sent.'); return; }
      err.classList.add('hidden');
      showToast('Sent for approval.', { variant: 'info' });
      txnDone('finance-coa');
      renderView('finance:chart-of-accounts');
    } finally {
      btn.disabled = false;
    }
  });
}
