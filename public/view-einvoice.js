// Stage 75.2.9: E-Invoice (IRN) - the IRN Exception Workbench (list first, one
// page per record) and Tax Integration Settings (one row per GSTIN, a page to
// set one up). Server: internal/server/handlers_einvoice.go, engines/einvoice*.go.
// Also the IRN badge the Sales Invoice list shows and the QR its print draws.

const esc = v => escapeHTMLText(v == null ? '' : String(v));

const TABS = [
  ['needs_action', 'Needs action'], ['queued', 'Queued'], ['unknown', 'Unknown'],
  ['rejected', 'Rejected'], ['acknowledged', 'Acknowledged'], ['cancelled', 'Cancelled']
];

const STATE_TEXT = {
  READY: 'Ready', QUEUED: 'Queued', SUBMITTING: 'Sending', ACKNOWLEDGED: 'IRN generated', REJECTED: 'Rejected',
  UNKNOWN: 'Unknown - being checked', RECONCILE: 'Checking', CANCEL_PENDING: 'Cancelling', CANCELLED: 'Cancelled',
  CANCEL_UNKNOWN: 'Cancel unknown', MANUAL_REVIEW: 'Manual review', MANUAL_IMPORT: 'Portal IRN - to verify',
  BLOCKED_CONFIG: 'Blocked - set-up', NOT_REQUIRED: 'Not required'
};
const STATE_BADGE = {
  ACKNOWLEDGED: 'badge-success', CANCELLED: 'badge-secondary', NOT_REQUIRED: 'badge-secondary', REJECTED: 'badge-danger',
  BLOCKED_CONFIG: 'badge-danger', MANUAL_REVIEW: 'badge-danger', UNKNOWN: 'badge-warning', CANCEL_UNKNOWN: 'badge-warning'
};

export function einvoiceStateBadge(state) {
  return `<span class="badge ${STATE_BADGE[state] || 'badge-warning'}">${esc(STATE_TEXT[state] || state)}</span>`;
}

const wb = { tab: 'needs_action', gstin: '', from: '', to: '', q: '', offset: 0 };
const PAGE = 50;

export async function renderEInvoiceView(container) {
  const tab = typeof currentViewTab === 'string' ? currentViewTab : '';
  if (tab === 'settings') return renderSettings(container);
  if (tab.startsWith('gstin-')) return renderProfileForm(container, decodeURIComponent(tab.slice(6)));
  if (tab.startsWith('doc-')) return renderDetail(container, tab.slice(4));
  return renderWorkbench(container);
}

function screen(container, title, actions, body) {
  const el = document.createElement('div');
  el.className = 'txn-screen';
  el.innerHTML = `
    <div class="txn-bar">
      <h1 class="page-title txn-title">${title}</h1>
      <div class="txn-actions">${actions || ''}</div>
    </div>
    <div class="txn-grid">${body}</div>`;
  container.appendChild(el);
  return el;
}

// ---------------------------------------------------------------------------
// Workbench
// ---------------------------------------------------------------------------
async function renderWorkbench(container) {
  const qs = new URLSearchParams({ tab: wb.tab, limit: PAGE, offset: wb.offset });
  ['gstin', 'from', 'to', 'q'].forEach(k => { if (wb[k]) qs.set(k, wb[k]); });
  const res = await apiFetch(`/api/v1/einvoice/documents?${qs}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load e-invoices.'); return; }
  const data = await res.json();
  const rows = data.rows || [];
  const chips = TABS.map(([key, label]) => `<button type="button" class="btn btn-sm ${wb.tab === key ? 'btn-primary' : 'btn-outline'}" data-einv-tab="${key}">${label} <b>${(data.counts || {})[key] || 0}</b></button>`).join(' ');
  const el = screen(container, 'E-Invoice (IRN)',
    `<button class="btn btn-outline" id="einv-run">Check now</button> <button class="btn btn-outline" data-go-settings>Settings</button>`,
    `<div class="einv-filters" style="display:flex;flex-wrap:wrap;gap:8px;align-items:end;margin-bottom:10px;">
       <div>${chips}</div>
       <input class="form-input" id="einv-q" placeholder="Number or IRN" value="${esc(wb.q)}" style="max-width:180px;">
       <input class="form-input" id="einv-gstin" placeholder="GSTIN" value="${esc(wb.gstin)}" style="max-width:170px;">
       <input class="form-input" type="date" id="einv-from" value="${esc(wb.from)}" aria-label="From date" style="max-width:150px;">
       <input class="form-input" type="date" id="einv-to" value="${esc(wb.to)}" aria-label="To date" style="max-width:150px;">
       <button class="btn btn-outline btn-sm" id="einv-apply">Apply</button>
     </div>
     <table>
       <thead><tr><th>Date</th><th>Type</th><th>Number</th><th>Seller GSTIN</th><th>State</th><th>IRN</th><th class="num">Tries</th><th>What to do</th><th></th></tr></thead>
       <tbody>${rows.length === 0
         ? `<tr><td colspan="9" style="text-align:center;color:var(--text-muted);">Nothing here. Posted invoices and credit notes of a GSTIN with e-invoicing on appear within a minute.</td></tr>`
         : rows.map(r => `<tr>
             <td>${esc(r.doc_date)}</td><td>${esc(r.doc_type)}</td>
             <td style="font-family:monospace;">${esc(r.doc_number)}</td><td style="font-family:monospace;">${esc(r.gstin)}</td>
             <td>${einvoiceStateBadge(r.state)}</td>
             <td style="font-family:monospace;" title="${esc(r.irn)}">${esc(r.irn ? r.irn.slice(0, 12) + '…' : '')}</td>
             <td class="num">${r.attempts || 0}</td>
             <td style="max-width:320px;">${esc(nextStep(r))}</td>
             <td><button class="action-btn" data-einv-open="${r.id}">Open</button></td></tr>`).join('')}
       </tbody>
     </table>
     <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:8px;">
       <span style="color:var(--text-muted);align-self:center;">${data.total || 0} in this view</span>
       <button class="btn btn-outline btn-sm" id="einv-prev" ${wb.offset === 0 ? 'disabled' : ''}>Previous</button>
       <button class="btn btn-outline btn-sm" id="einv-next" ${wb.offset + PAGE >= (data.total || 0) ? 'disabled' : ''}>Next</button>
     </div>`);
  const redraw = () => renderView('einvoice');
  el.querySelectorAll('[data-einv-tab]').forEach(b => b.addEventListener('click', () => { wb.tab = b.dataset.einvTab; wb.offset = 0; redraw(); }));
  el.querySelector('#einv-apply').addEventListener('click', () => {
    wb.q = el.querySelector('#einv-q').value.trim(); wb.gstin = el.querySelector('#einv-gstin').value.trim().toUpperCase();
    wb.from = el.querySelector('#einv-from').value; wb.to = el.querySelector('#einv-to').value; wb.offset = 0; redraw();
  });
  el.querySelector('#einv-prev').addEventListener('click', () => { wb.offset = Math.max(0, wb.offset - PAGE); redraw(); });
  el.querySelector('#einv-next').addEventListener('click', () => { wb.offset += PAGE; redraw(); });
  el.querySelectorAll('[data-einv-open]').forEach(b => b.addEventListener('click', () => renderView('einvoice:doc-' + b.dataset.einvOpen)));
  el.querySelector('[data-go-settings]').addEventListener('click', () => renderView('einvoice:settings'));
  el.querySelector('#einv-run').addEventListener('click', async () => {
    // One worker pass now instead of the next minute's: newly posted documents
    // are registered and the queue is worked (nothing unqueued is sent).
    const resp = await apiFetch('/api/v1/einvoice/run', { method: 'POST' });
    if (resp && !resp.ok) { await showApiError(resp, 'The e-invoice check failed.'); return; }
    redraw();
  });
}

function nextStep(r) {
  if (r.issues && r.issues.length) return `${r.issues.length} thing(s) to fix - open it`;
  switch (r.state) {
    case 'READY': return 'Valid - Submit to generate the IRN';
    case 'QUEUED': case 'SUBMITTING': return 'Being sent';
    case 'UNKNOWN': return 'The IRP did not answer - it is looked up before anything is sent again';
    case 'REJECTED': return r.last_error || 'Refused by the IRP - fix and Validate';
    case 'MANUAL_REVIEW': case 'BLOCKED_CONFIG': return r.last_error || 'Needs a person';
    case 'MANUAL_IMPORT': return 'A second person checks the portal IRN (Approvals)';
    case 'ACKNOWLEDGED': return r.ack_no ? `Ack ${r.ack_no}` : '';
    case 'CANCEL_PENDING': return 'Cancel being sent';
    default: return r.reason || '';
  }
}

// ---------------------------------------------------------------------------
// One record
// ---------------------------------------------------------------------------
async function renderDetail(container, id) {
  const res = await apiFetch(`/api/v1/einvoice/documents/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load the e-invoice.'); renderView('einvoice'); return; }
  const d = await res.json();
  const r = d.record;
  const fact = (k, v) => v ? `<div><span class="stat-label">${k}</span><div>${v}</div></div>` : '';
  const can = {
    validate: ['READY', 'REJECTED', 'BLOCKED_CONFIG', 'NOT_REQUIRED'].includes(r.state),
    submit: (r.state === 'READY' && !(r.issues || []).length) || ['BLOCKED_CONFIG', 'MANUAL_REVIEW'].includes(r.state),
    check: r.state === 'UNKNOWN',
    cancel: r.state === 'ACKNOWLEDGED',
    manual: ['READY', 'QUEUED', 'REJECTED', 'UNKNOWN', 'BLOCKED_CONFIG', 'MANUAL_REVIEW'].includes(r.state)
  };
  const btn = (key, label, cls = 'btn-outline') => can[key] ? `<button class="btn ${cls}" data-einv-act="${key}">${label}</button>` : '';
  const table = (rows, cols) => rows.length ? `<table><thead><tr>${cols.map(c => `<th>${c[1]}</th>`).join('')}</tr></thead><tbody>${
    rows.map(x => `<tr>${cols.map(c => `<td>${esc(c[0] === 'at' ? fmtTime(x.at) : x[c[0]])}</td>`).join('')}</tr>`).join('')}</tbody></table>` : '<p style="color:var(--text-muted);">None yet.</p>';
  const el = screen(container, `E-Invoice ${esc(r.doc_type)} ${esc(r.doc_number)}`,
    `<button class="btn btn-outline" data-einv-back>Back to list</button> ${btn('validate', 'Validate')} ${btn('submit', r.state === 'READY' ? 'Submit' : 'Retry', 'btn-primary')} ${btn('check', 'Check status', 'btn-primary')} ${btn('cancel', 'Cancel IRN')} ${btn('manual', 'Enter portal IRN')}`,
    `<div class="oms-detail-summary" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:10px;margin-bottom:14px;">
       ${fact('State', einvoiceStateBadge(r.state))}
       ${fact('Document', esc(`${r.source_doctype} · ${r.doc_date}`))}
       ${fact('Seller GSTIN', esc(r.gstin))}
       ${fact('Why', esc(r.reason))}
       ${fact('Rules', r.rule_version ? 'v' + r.rule_version : '')}
       ${fact('IRN', r.irn ? `<span style="font-family:monospace;word-break:break-all;">${esc(r.irn)}</span>` : '')}
       ${fact('Ack No / Date', r.ack_no ? esc(`${r.ack_no} · ${fmtTime(r.ack_date)}`) : '')}
       ${fact('Made by', r.source === 'manual' ? 'Portal (entered by hand, verified)' : '')}
       ${fact('Tries', String(r.attempts || 0))}
       ${fact('Next try', r.next_attempt_at ? esc(fmtTime(r.next_attempt_at)) : '')}
       ${fact('Payload hash', r.payload_hash ? `<span style="font-family:monospace;">${esc(r.payload_hash.slice(0, 16))}…</span>` : '')}
     </div>
     ${(r.issues || []).length ? `<h3>To fix before it can be sent</h3>${table(r.issues, [['field', 'Field'], ['problem', 'Problem'], ['fix', 'How to fix']])}` : ''}
     ${r.last_error && !(r.issues || []).length ? `<div class="login-error" style="margin:8px 0;">${esc(r.last_error)}</div>` : ''}
     ${r.signed_qr ? `<h3>Signed QR</h3><div id="einv-qr" style="width:180px;"></div>` : ''}
     <div id="einv-form"></div>
     <h3>Calls to the IRP</h3>${table(d.attempts || [], [['at', 'When'], ['action', 'Call'], ['outcome', 'Result'], ['error_code', 'Code'], ['error_message', 'Message'], ['duration_ms', 'ms']])}
     <h3>History</h3>${table(d.events || [], [['at', 'When'], ['event', 'Event'], ['from_state', 'From'], ['to_state', 'To'], ['detail', 'Detail'], ['actor', 'By']])}
     ${d.payload ? `<details style="margin-top:10px;"><summary>The e-invoice JSON (frozen at posting)</summary><pre style="white-space:pre-wrap;font-size:12px;">${esc(JSON.stringify(d.payload, null, 2))}</pre></details>` : ''}`);
  el.querySelector('[data-einv-back]').addEventListener('click', () => renderView('einvoice'));
  if (r.signed_qr) einvoiceQRInto(el.querySelector('#einv-qr'), r.signed_qr);
  const act = async (action, body) => {
    const resp = await apiFetch(`/api/v1/einvoice/documents/${r.id}/${action}`, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
    if (!resp) return;
    if (!resp.ok) { await showApiError(resp, 'The e-invoice action failed.'); return; }
    const after = await resp.json();
    showToast(`${r.doc_number}: ${STATE_TEXT[after.state] || after.state}.`, { variant: 'success', title: 'E-Invoice' });
    renderView('einvoice:doc-' + r.id);
  };
  el.querySelectorAll('[data-einv-act]').forEach(b => b.addEventListener('click', () => {
    const key = b.dataset.einvAct;
    if (key === 'validate') return act('validate');
    if (key === 'submit') return act('submit');
    if (key === 'check') return act('check');
    if (key === 'cancel') return cancelForm(el, r, act);
    if (key === 'manual') return manualForm(el, r, act);
  }));
}

function cancelForm(el, r, act) {
  const box = el.querySelector('#einv-form');
  box.innerHTML = `<div class="table-panel" style="margin:12px 0;padding:12px;">
    <h3>Cancel IRN</h3>
    <p style="color:var(--text-muted);">The IRP allows a cancel within 24 hours of the acknowledgement. Another Accountant approves it (Approvals); the number is never used again. After 24 hours, issue a credit note.</p>
    <div class="po-header-grid">
      <div class="form-group"><label class="form-label" for="einv-c-code">Reason code</label>
        <select class="form-select" id="einv-c-code"><option value="1">1 - Duplicate</option><option value="2">2 - Data entry mistake</option><option value="3">3 - Order cancelled</option><option value="4">4 - Others</option></select></div>
      <div class="form-group" style="grid-column:span 3;"><label class="form-label" for="einv-c-reason">Reason</label><input class="form-input" id="einv-c-reason" maxlength="100"></div>
    </div>
    <button class="btn btn-primary" id="einv-c-go">Send for approval</button></div>`;
  box.querySelector('#einv-c-go').addEventListener('click', () => act('cancel', { code: box.querySelector('#einv-c-code').value, reason: box.querySelector('#einv-c-reason').value }));
}

function manualForm(el, r, act) {
  const box = el.querySelector('#einv-form');
  box.innerHTML = `<div class="table-panel" style="margin:12px 0;padding:12px;">
    <h3>Enter the IRN made on the portal</h3>
    <p style="color:var(--text-muted);">For an outage: the IRN was generated on the IRP's own portal. Copy what the portal shows; a second person checks it against the portal before it counts (Approvals).</p>
    <div class="po-header-grid">
      <div class="form-group" style="grid-column:span 2;"><label class="form-label" for="einv-m-irn">IRN (64 characters)</label><input class="form-input" id="einv-m-irn" maxlength="64" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="einv-m-ack">Ack No</label><input class="form-input" id="einv-m-ack" autocomplete="off"></div>
      <div class="form-group"><label class="form-label" for="einv-m-date">Ack Date (YYYY-MM-DD HH:MM)</label><input class="form-input" id="einv-m-date" autocomplete="off"></div>
    </div>
    <div class="form-group"><label class="form-label" for="einv-m-qr">Signed QR (the text, as the portal gives it)</label><textarea class="form-textarea" id="einv-m-qr" rows="3"></textarea></div>
    <div class="form-group"><label class="form-label" for="einv-m-why">Why it was made on the portal</label><input class="form-input" id="einv-m-why"></div>
    <button class="btn btn-primary" id="einv-m-go">Send for verification</button></div>`;
  const v = id => box.querySelector(id).value.trim();
  box.querySelector('#einv-m-go').addEventListener('click', () => act('manual-import', { irn: v('#einv-m-irn'), ack_no: v('#einv-m-ack'), ack_date: v('#einv-m-date'), signed_qr: v('#einv-m-qr'), reason: v('#einv-m-why') }));
}

function fmtTime(t) {
  if (!t) return '';
  const d = new Date(t);
  return isNaN(d) ? String(t) : d.toLocaleString();
}

// ---------------------------------------------------------------------------
// Tax Integration Settings
// ---------------------------------------------------------------------------
async function renderSettings(container) {
  const res = await apiFetch('/api/v1/einvoice/profiles');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Could not load the e-invoice set-up.'); return; }
  const { environment, profiles = [] } = await res.json();
  const el = screen(container, 'E-Invoice Settings',
    `<span class="badge ${environment === 'production' ? 'badge-danger' : 'badge-secondary'}" title="IRP_ENVIRONMENT on this server">${environment === 'production' ? 'Production' : 'Sandbox'}</span> <button class="btn btn-outline" data-einv-wb>Workbench</button> <button class="btn btn-primary" id="einv-new">+ New GSTIN</button>`,
    `<table>
       <thead><tr><th>GSTIN</th><th>Legal entity</th><th>Provider</th><th>E-invoicing</th><th>Auto-submit</th><th>Turnover</th><th>Types</th><th>Rules</th><th>Connection</th><th></th></tr></thead>
       <tbody>${profiles.length === 0
         ? `<tr><td colspan="10" style="text-align:center;color:var(--text-muted);">No GSTIN is set up. Use <b>+ New GSTIN</b> for each GSTIN that must e-invoice (turnover above ₹5 crore).</td></tr>`
         : profiles.map(p => `<tr>
             <td style="font-family:monospace;">${esc(p.gstin)}</td><td>${esc(p.legal_entity)}</td>
             <td>${esc(p.provider === 'fake' ? 'Test provider' : p.provider)}${p.credential_set ? ' · credentials stored' : ''}</td>
             <td>${p.enabled ? '<span class="badge badge-success">On</span>' : '<span class="badge badge-secondary">Off</span>'}</td>
             <td>${p.auto_submit ? 'On' : 'Off'}</td><td>${esc(p.turnover_band)}</td><td>${esc((p.document_types || []).join(', '))}</td>
             <td>v${p.rule_version} from ${esc(p.effective_from)}</td>
             <td>${esc(p.test_status)}${p.tested_at ? `<div style="font-size:11px;color:var(--text-muted);">${esc(fmtTime(p.tested_at))}</div>` : ''}</td>
             <td><button class="action-btn" data-einv-edit="${esc(p.gstin)}">Edit</button> <button class="action-btn" data-einv-test="${esc(p.gstin)}">Test connection</button></td></tr>`).join('')}
       </tbody>
     </table>`);
  el.querySelector('#einv-new').addEventListener('click', () => renderView('einvoice:gstin-'));
  el.querySelector('[data-einv-wb]').addEventListener('click', () => renderView('einvoice'));
  el.querySelectorAll('[data-einv-edit]').forEach(b => b.addEventListener('click', () => renderView('einvoice:gstin-' + encodeURIComponent(b.dataset.einvEdit))));
  el.querySelectorAll('[data-einv-test]').forEach(b => b.addEventListener('click', async () => {
    const resp = await apiFetch(`/api/v1/einvoice/profiles/${encodeURIComponent(b.dataset.einvTest)}/test`, { method: 'POST' });
    if (!resp) return;
    if (!resp.ok) { await showApiError(resp, 'The connection test failed.'); return; }
    const p = await resp.json();
    showToast(`${p.gstin}: ${p.test_status}`, { variant: p.test_status === 'Connected' ? 'success' : 'warning', title: 'E-Invoice' });
    renderView('einvoice:settings');
  }));
}

const ALL_TYPES = [['INV', 'Invoice'], ['CRN', 'Credit note'], ['DBN', 'Debit note']];
const ALL_SUPPLY = [['B2B', 'B2B'], ['SEZWP', 'SEZ with payment'], ['SEZWOP', 'SEZ without payment'], ['EXPWP', 'Export with payment'], ['EXPWOP', 'Export without payment'], ['DEXP', 'Deemed export']];

async function renderProfileForm(container, gstin) {
  let p = { gstin: '', legal_entity: '', provider: 'fake', enabled: false, auto_submit: false, turnover_band: '5-10CR', document_types: ['INV', 'CRN', 'DBN'], supply_types: ALL_SUPPLY.map(s => s[0]), inter_gstin: 'REQUIRED', whitelisted_ip: '', effective_from: new Date().toISOString().slice(0, 10) };
  if (gstin) {
    const res = await apiFetch('/api/v1/einvoice/profiles');
    if (!res) return;
    const found = res.ok ? ((await res.json()).profiles || []).find(x => x.gstin === gstin) : null;
    if (found) p = found;
  }
  const checks = (name, all, on) => all.map(([v, l]) => `<label style="margin-right:12px;"><input type="checkbox" name="${name}" value="${v}" ${on.includes(v) ? 'checked' : ''}> ${l}</label>`).join('');
  const el = screen(container, gstin ? `E-Invoice Settings › ${esc(gstin)}` : 'E-Invoice Settings › New GSTIN',
    `<button class="btn btn-outline" data-einv-back>Back</button> <button class="btn btn-primary" id="einv-save">Save</button>`,
    `<div class="po-header-grid">
       <div class="form-group"><label class="form-label" for="einv-gstin-in">GSTIN</label><input class="form-input" id="einv-gstin-in" maxlength="15" value="${esc(p.gstin)}" ${gstin ? 'readonly' : ''} autocomplete="off"></div>
       <div class="form-group"><label class="form-label" for="einv-le">Legal entity</label><input class="form-input" id="einv-le" value="${esc(p.legal_entity)}"></div>
       <div class="form-group"><label class="form-label" for="einv-provider">Provider</label><select class="form-select" id="einv-provider"><option value="fake">Test provider (sandbox only)</option></select></div>
       <div class="form-group"><label class="form-label" for="einv-turnover">Turnover (AATO)</label><select class="form-select" id="einv-turnover"><option value="5-10CR" ${p.turnover_band === '5-10CR' ? 'selected' : ''}>₹5 - 10 crore</option><option value="10CR+" ${p.turnover_band === '10CR+' ? 'selected' : ''}>₹10 crore and above (30-day rule)</option></select></div>
       <div class="form-group"><label class="form-label" for="einv-from">Effective from</label><input class="form-input" type="date" id="einv-from" value="${esc(p.effective_from)}"></div>
       <div class="form-group"><label class="form-label" for="einv-inter">Supplies to another GSTIN of the same PAN</label><select class="form-select" id="einv-inter"><option value="REQUIRED" ${p.inter_gstin !== 'NOT_REQUIRED' ? 'selected' : ''}>E-invoice them</option><option value="NOT_REQUIRED" ${p.inter_gstin === 'NOT_REQUIRED' ? 'selected' : ''}>Do not e-invoice them</option></select></div>
       <div class="form-group"><label class="form-label" for="einv-ip">Whitelisted server IP (as given to the IRP)</label><input class="form-input" id="einv-ip" value="${esc(p.whitelisted_ip)}" autocomplete="off"></div>
     </div>
     <div class="form-group"><span class="form-label">Document types</span><div>${checks('einv-types', ALL_TYPES, p.document_types || [])}</div></div>
     <div class="form-group"><span class="form-label">Supply types</span><div>${checks('einv-supply', ALL_SUPPLY, p.supply_types || [])}</div></div>
     <div class="form-group"><label><input type="checkbox" id="einv-enabled" ${p.enabled ? 'checked' : ''}> E-invoicing is on for this GSTIN</label></div>
     <div class="form-group"><label><input type="checkbox" id="einv-auto" ${p.auto_submit ? 'checked' : ''}> Send each posted invoice automatically (otherwise Submit from the workbench)</label></div>
     <h3>Provider credentials</h3>
     <p style="color:var(--text-muted);">Stored encrypted and never shown again${p.credential_set ? ' - <b>stored</b>; leave blank to keep them' : ''}. The test provider needs none.</p>
     <div class="po-header-grid">
       <div class="form-group"><label class="form-label" for="einv-user">API user name</label><input class="form-input" id="einv-user" autocomplete="off"></div>
       <div class="form-group"><label class="form-label" for="einv-pass">API password</label><input class="form-input" type="password" id="einv-pass" autocomplete="new-password"></div>
       <div class="form-group"><label class="form-label" for="einv-cid">Client ID</label><input class="form-input" id="einv-cid" autocomplete="off"></div>
       <div class="form-group"><label class="form-label" for="einv-csec">Client secret</label><input class="form-input" type="password" id="einv-csec" autocomplete="new-password"></div>
     </div>`);
  el.querySelector('[data-einv-back]').addEventListener('click', () => renderView('einvoice:settings'));
  el.querySelector('#einv-save').addEventListener('click', async () => {
    const v = id => el.querySelector(id).value.trim();
    const checked = name => [...el.querySelectorAll(`input[name="${name}"]:checked`)].map(i => i.value);
    const body = {
      gstin: v('#einv-gstin-in').toUpperCase(), legal_entity: v('#einv-le'), provider: v('#einv-provider'),
      turnover_band: v('#einv-turnover'), effective_from: v('#einv-from'), inter_gstin: v('#einv-inter'), whitelisted_ip: v('#einv-ip'),
      document_types: checked('einv-types'), supply_types: checked('einv-supply'),
      enabled: el.querySelector('#einv-enabled').checked, auto_submit: el.querySelector('#einv-auto').checked,
      credentials: { username: v('#einv-user'), password: v('#einv-pass'), client_id: v('#einv-cid'), client_secret: v('#einv-csec') }
    };
    const resp = await apiFetch('/api/v1/einvoice/profiles', { method: 'POST', body: JSON.stringify(body) });
    if (!resp) return;
    if (!resp.ok) { await showApiError(resp, 'Could not save the GSTIN.'); return; }
    const saved = await resp.json();
    showToast(`${saved.gstin} saved (rules v${saved.rule_version}).`, { variant: 'success', title: 'E-Invoice' });
    renderView('einvoice:settings');
  });
}

// ---------------------------------------------------------------------------
// Badge and QR, used by the Sales Invoice screen and its print
// ---------------------------------------------------------------------------

// einvoiceBadgesInto adds the IRN state to each element carrying
// data-einv-id, from one call for the whole list. Silent when the caller may
// not see e-invoices or none exist. Pass the cells, collected before any
// await: renderView builds a screen in a scratch container and then moves its
// nodes, so a container queried later is empty.
export async function einvoiceBadgesInto(cells, doctype) {
  if (!cells || !cells.length) return;
  let badges = {};
  try {
    const res = await fetch(`/api/v1/einvoice/badges/${encodeURIComponent(doctype)}`, { headers: authHeaders() });
    if (!res.ok) return;
    badges = await res.json() || {};
  } catch (e) { return; }
  cells.forEach(c => {
    const b = badges[c.dataset.einvId];
    if (b && b.state !== 'NOT_REQUIRED') c.insertAdjacentHTML('beforeend', ` ${einvoiceStateBadge(b.state)}`);
  });
}

function authHeaders() {
  const h = { Authorization: 'Bearer ' + (localStorage.getItem('erp_token') || '') };
  const t = localStorage.getItem('erp_tenant_id');
  if (t) h['X-Tenant-ID'] = t;
  return h;
}

// einvoiceFor returns one document's e-invoice (with its signed QR), or null.
export async function einvoiceFor(doctype, id) {
  try {
    const res = await fetch(`/api/v1/einvoice/badges/${encodeURIComponent(doctype)}?id=${encodeURIComponent(id)}`, { headers: authHeaders() });
    if (!res.ok) return null;
    return (await res.json() || {})[id] || null;
  } catch (e) { return null; }
}

// einvoiceQRInto draws the IRP's signed QR text as an SVG - the QR is the
// IRP's own, never made up here (sticker-engine.js's QR encoder).
export async function einvoiceQRInto(el, text) {
  if (!el || !text) return;
  const { encodeQR } = await import('./sticker-engine.js' + new URL(import.meta.url).search);
  const qr = encodeQR(text, 'M');
  if (!qr) { el.textContent = 'QR too long to draw'; return; }
  const n = qr.size, q = 4, cells = [];
  qr.modules.forEach((row, y) => row.forEach((on, x) => { if (on) cells.push(`M${x + q},${y + q}h1v1h-1z`); }));
  el.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n + 2 * q} ${n + 2 * q}" width="100%" shape-rendering="crispEdges" role="img" aria-label="E-invoice QR"><rect width="100%" height="100%" fill="#fff"/><path d="${cells.join('')}" fill="#000"/></svg>`;
}

// einvoicePrintBlock is what a tax invoice print adds: IRN, Ack and the QR
// once acknowledged; an "IRN pending" banner while it is on its way; nothing
// for a sale that is not e-invoiced (B2C prints are unchanged).
export function einvoicePrintBlock(rec) {
  if (!rec || rec.state === 'NOT_REQUIRED' || rec.state === 'CANCELLED') return '';
  if (rec.state !== 'ACKNOWLEDGED') return '<div class="invoice-draft">IRN PENDING</div>';
  const when = rec.ack_date ? new Date(rec.ack_date).toLocaleString() : '';
  return `<table><tr><td class="invoice-key">IRN</td><td style="word-break:break-all;">${esc(rec.irn)}</td></tr>
    <tr><td class="invoice-key">Ack No</td><td>${esc(rec.ack_no)}</td></tr><tr><td class="invoice-key">Ack Date</td><td>${esc(when)}</td></tr></table>
    <div class="einv-print-qr" style="width:150px;margin-top:8px;"></div>`;
}
