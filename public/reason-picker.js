// Stage 74.1.1: the reason picker every exception screen shares - the reason
// codes usable today for one action (GET /api/v1/reason-codes), as a select
// beside an optional note. The server checks the code again on save and,
// when the client requires codes, refuses a typed reason alone.
//
//   const r = await loadReasons('Goods Receipt Rejection');
//   html += reasonSelectHTML(r, { value, attrs: 'data-field="rejection_reason_code"' });

const cache = new Map(); // category -> { at, data }
const TTL = 60 * 1000;

export async function loadReasons(category) {
  const hit = cache.get(category);
  if (hit && Date.now() - hit.at < TTL) return hit.data;
  const res = await apiFetch(`/api/v1/reason-codes?category=${encodeURIComponent(category)}`);
  const data = res && res.ok ? await res.json() : { codes: [], required: false };
  cache.set(category, { at: Date.now(), data });
  return data;
}

export function reasonSelectHTML(reasons, opts = {}) {
  const codes = (reasons && reasons.codes) || [];
  const value = opts.value || '';
  const blank = reasons && reasons.required ? 'Choose a reason' : 'Reason (choose, or type below)';
  return `<select class="form-select" ${opts.attrs || ''} aria-label="${escapeHTMLText(opts.label || 'Reason')}">
    <option value="">${escapeHTMLText(blank)}</option>
    ${codes.map(c => `<option value="${escapeHTMLText(c.code)}"${c.code === value ? ' selected' : ''} title="${escapeHTMLText(c.when_to_use || '')}">${escapeHTMLText(c.label)}${c.evidence_required ? ' (attach a file)' : ''}</option>`).join('')}
  </select>`;
}

// chooseReason asks for a reason in a small dialog - the codes as a list and
// an optional note - and resolves to { code, text }, or null when cancelled.
// The server decides whether a typed reason alone is enough.
export async function chooseReason({ category, title, question }) {
  const reasons = await loadReasons(category);
  return new Promise(resolve => {
    const returnFocus = document.activeElement;
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay open';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.innerHTML = `
      <div class="modal-container">
        <div class="modal-header"><h3 class="modal-title"></h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
        <form novalidate>
          <div class="modal-body">
            <p class="rp-question"></p>
            <div class="form-group">${reasonSelectHTML(reasons, { attrs: 'id="rp-code"', label: 'Reason' })}</div>
            <div class="form-group"><input id="rp-text" class="form-input" autocomplete="off" placeholder="${reasons.codes.length ? 'note (optional)' : 'reason'}" aria-label="Reason note"></div>
            <div class="login-error hidden" role="alert"></div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" data-rp-cancel>Cancel</button>
            <button type="submit" class="btn btn-primary">OK</button>
          </div>
        </form>
      </div>`;
    overlay.querySelector('.modal-title').textContent = title || 'Reason';
    overlay.querySelector('.rp-question').textContent = question || '';
    const close = (value) => {
      overlay.remove();
      if (returnFocus && returnFocus.focus) returnFocus.focus();
      resolve(value);
    };
    overlay.querySelector('.modal-close').addEventListener('click', () => close(null));
    overlay.querySelector('[data-rp-cancel]').addEventListener('click', () => close(null));
    overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(null); });
    overlay.querySelector('form').addEventListener('submit', e => {
      e.preventDefault();
      const code = overlay.querySelector('#rp-code').value;
      const text = overlay.querySelector('#rp-text').value.trim();
      if (!code && (!text || reasons.required)) {
        const err = overlay.querySelector('.login-error');
        err.textContent = reasons.required ? 'Choose a reason from the list.' : 'Choose a reason, or type one.';
        err.classList.remove('hidden');
        return;
      }
      close({ code, text });
    });
    document.body.appendChild(overlay);
    overlay.querySelector('#rp-code').focus();
  });
}
