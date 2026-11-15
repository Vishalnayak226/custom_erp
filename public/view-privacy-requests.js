// 49.6.8: a customer's, employee's or vendor's privacy requests from their
// list row - open one, approve or reject it (a second admin; the server refuses
// the requester), then run it. A customer request may also be a correction
// (with the corrected values) or a consent withdrawal; an employee or vendor
// request is access, export or erasure only (170.39), and the server says which
// fields it keeps. An export or access run saves the subject's copy as JSON.
// Loaded on demand by the lists; shared services (apiFetch, showApiError,
// showToast, escapeHTMLText, showCustomPrompt, showCustomConfirm) are globals.

const VQ = new URL(import.meta.url).search;

const PR_TYPES = [
  ['access', 'Access'], ['export', 'Export'], ['correction', 'Correction'],
  ['erasure', 'Erasure'], ['anonymize', 'Anonymise'], ['consent_withdraw', 'Consent withdrawal'],
];
const PR_TYPE_LABEL = Object.fromEntries(PR_TYPES);
// What 170.39 runs for an employee or a vendor.
const PR_STAFF_TYPES = new Set(['access', 'export', 'erasure', 'anonymize']);

// The Customer fields a correction may set (customerCorrectableFields in
// engines/privacy_rights_sweep.go); the server holds each to the Customer's rules.
const PR_FIELDS = [
  ['name', 'Name'], ['phone', 'Phone'], ['phone_country', 'Phone country'], ['email', 'Email'],
  ['address', 'Address'], ['address_line2', 'Address 2'], ['city', 'City'], ['state', 'State'],
  ['state_code', 'State code'], ['pincode', 'PIN code'], ['pan', 'PAN'], ['gstin', 'GSTIN'],
  ['date_of_birth', 'Birth date', 'date'], ['date_of_anniversary', 'Anniversary', 'date'], ['gender', 'Gender'],
];

async function prJson(path, opts, failText) {
  const res = await apiFetch(path, opts);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, failText); return null; }
  return res.json();
}

const prDate = v => (v ? new Date(v).toLocaleDateString() : '');

function prActions(r) {
  const id = escapeHTMLText(r.ID);
  if (r.Status === 'Pending') {
    return `<button type="button" class="btn btn-outline btn-sm s-c6b76b" data-pr-decide="1" data-pr-id="${id}">Approve</button>`
      + `<button type="button" class="btn btn-outline btn-sm" data-pr-decide="0" data-pr-id="${id}">Reject</button>`;
  }
  if (r.Status === 'Approved') return `<button type="button" class="btn btn-primary btn-sm" data-pr-run data-pr-id="${id}" data-pr-type="${escapeHTMLText(r.RequestType)}">Run</button>`;
  return '';
}

function prCorrections(c) {
  const keys = Object.keys(c || {});
  return keys.length ? `<div class="pr-changes">${keys.map(k => `${escapeHTMLText(k)}: ${escapeHTMLText(String(c[k]))}`).join(' · ')}</div>` : '';
}

function prListHTML(list) {
  if (!list.length) return '<p class="pr-empty">No requests</p>';
  return `<div class="table-panel"><table class="data-table"><thead><tr><th>Type</th><th>Status</th><th>By</th><th>On</th><th></th></tr></thead><tbody>${list.map(r => `
    <tr><td>${escapeHTMLText(PR_TYPE_LABEL[r.RequestType] || r.RequestType)}${prCorrections(r.Corrections)}</td>
      <td>${escapeHTMLText(r.Status)}${r.DecidedBy ? ` · ${escapeHTMLText(r.DecidedBy)}` : ''}</td>
      <td>${escapeHTMLText(r.RequestedBy)}</td><td>${escapeHTMLText(prDate(r.CompletedAt || r.RequestedAt))}</td>
      <td class="pr-row-actions">${prActions(r)}</td></tr>`).join('')}</tbody></table></div>`;
}

function prFormHTML(doctype) {
  const types = doctype === 'Customer' ? PR_TYPES : PR_TYPES.filter(([v]) => PR_STAFF_TYPES.has(v));
  return `<form class="pr-form" novalidate>
    <label class="form-label">Type<select class="form-select" name="request_type">${types.map(([v, l]) => `<option value="${v}">${l}</option>`).join('')}</select></label>
    <label class="form-label">Reason<input type="text" class="form-input" name="reason" maxlength="500"></label>
    <fieldset class="pr-fields" hidden><legend class="form-label">New values</legend>
      ${PR_FIELDS.map(([f, l, t]) => `<label class="form-label">${l}<input type="${t || 'text'}" class="form-input" data-pr-field="${f}"></label>`).join('')}
    </fieldset>
  </form>`;
}

async function prSaveCopy(data, subjectId, requestId) {
  const { downloadBlob } = await import('./print-pdf.js' + VQ);
  downloadBlob(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }), `privacy-${subjectId}-${requestId}.json`);
}

export async function openPrivacyRequests(subjectId, doctype = 'Customer') {
  document.getElementById('pr-dialog')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pr-dialog';
  overlay.innerHTML = `<div class="modal-container pr-dialog" role="dialog" aria-modal="true" aria-labelledby="pr-title">
      <div class="modal-header"><h3 class="modal-title" id="pr-title">Privacy · ${escapeHTMLText(subjectId)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-pr-close>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
        </button></div>
      <div class="modal-body" data-pr-body></div>
      <div class="modal-footer" data-pr-foot></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
  const body = overlay.querySelector('[data-pr-body]');
  const foot = overlay.querySelector('[data-pr-foot]');
  const closeBtn = '<button type="button" class="btn btn-outline" data-pr-close>Close</button>';
  const bindClose = () => overlay.querySelectorAll('[data-pr-close]').forEach(b => b.addEventListener('click', close));
  const q = `subject_doctype=${encodeURIComponent(doctype)}&subject_id=${encodeURIComponent(subjectId)}`;

  async function showList() {
    body.innerHTML = '<p class="pr-empty">Loading…</p>';
    foot.innerHTML = `${closeBtn}<button type="button" class="btn btn-primary" data-pr-new>New</button>`;
    bindClose();
    foot.querySelector('[data-pr-new]').addEventListener('click', showForm);
    const list = await prJson(`/api/v1/admin/privacy/requests?${q}`, undefined, 'Requests could not be loaded.');
    if (!list) { body.innerHTML = '<p class="pr-empty">Not loaded</p>'; return; }
    body.innerHTML = prListHTML(list);
  }

  function showForm() {
    body.innerHTML = prFormHTML(doctype);
    foot.innerHTML = `<button type="button" class="btn btn-outline" data-pr-back>Back</button><button type="button" class="btn btn-primary" data-pr-save>Save</button>`;
    const form = body.querySelector('form');
    const fields = form.querySelector('.pr-fields');
    form.request_type.addEventListener('change', () => { fields.hidden = form.request_type.value !== 'correction'; });
    foot.querySelector('[data-pr-back]').addEventListener('click', showList);
    const save = foot.querySelector('[data-pr-save]');
    save.addEventListener('click', async () => {
      const payload = { subject_doctype: doctype, subject_id: subjectId, request_type: form.request_type.value, reason: form.reason.value.trim() };
      if (payload.request_type === 'correction') {
        const corrections = {};
        form.querySelectorAll('[data-pr-field]').forEach(i => { if (i.value.trim()) corrections[i.dataset.prField] = i.value.trim(); });
        if (!Object.keys(corrections).length) { showToast('Enter at least one new value.', { variant: 'error' }); return; }
        payload.corrections = corrections;
      }
      save.disabled = true;
      const out = await prJson('/api/v1/admin/privacy/requests', { method: 'POST', body: JSON.stringify(payload) }, 'Not saved.');
      save.disabled = false;
      if (out) { showToast('Request opened.', { variant: 'success' }); showList(); }
    });
  }

  body.addEventListener('click', async e => {
    const decide = e.target.closest('[data-pr-decide]');
    const run = e.target.closest('[data-pr-run]');
    if (decide) {
      const approve = decide.dataset.prDecide === '1';
      let note = '';
      if (!approve) {
        note = (await showCustomPrompt('Reason for rejection:', '', 'Reject')) || '';
        if (!note.trim()) return;
      }
      decide.disabled = true;
      const out = await prJson(`/api/v1/admin/privacy/requests/${encodeURIComponent(decide.dataset.prId)}/decide`, { method: 'POST', body: JSON.stringify({ approve, note }) }, 'Not decided.');
      if (out) { showToast(approve ? 'Approved.' : 'Rejected.', { variant: 'success' }); showList(); } else decide.disabled = false;
    } else if (run) {
      const type = run.dataset.prType;
      if ((type === 'erasure' || type === 'anonymize') && !(await showCustomConfirm(`Remove this ${doctype.toLowerCase()}'s personal data? This cannot be undone.`, 'Run request', { typed: subjectId }))) return;
      run.disabled = true;
      const out = await prJson(`/api/v1/admin/privacy/requests/${encodeURIComponent(run.dataset.prId)}/execute`, { method: 'POST', body: '{}' }, 'Not run.');
      if (out) {
        if (out.data) await prSaveCopy(out.data, subjectId, run.dataset.prId);
        showToast(out.data ? 'Done. Copy saved.' : 'Done.', { variant: 'success' });
        showList();
      } else run.disabled = false;
    }
  });

  showList();
}

window.openPrivacyRequests = openPrivacyRequests;
