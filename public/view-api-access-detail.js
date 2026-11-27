// 205.13 / 205.16 / 205.19 / 205.21: a key's detail page and the helpers the
// API access list shares with it. Lazy module (loaded by view-api-access.js).
// Routes: GET/PUT/POST/DELETE /api/v1/api-access/keys/{id}/... (plan, "Interfaces").
// 297.1: imported with this module's own ?v= (a static import carries none, so a release would not reach it).
const { buildModuleGrid } = await import('./view-api-access-grid.js' + new URL(import.meta.url).search);
const { copyText, downloadText } = await import('./api-docs-viewer.js' + new URL(import.meta.url).search);

export const BASE = '/api/v1/api-access';
const esc = (s) => escapeHTMLText(s);

export async function call(path, method = 'GET', body) {
  const opts = { method };
  if (body !== undefined) opts.body = JSON.stringify(body);
  return apiFetch(BASE + path, opts);
}

// rows: the answer may be an array or wrap it in a named field.
export function rowsOf(body, ...names) {
  if (Array.isArray(body)) return body;
  for (const n of names) if (body && Array.isArray(body[n])) return body[n];
  return [];
}

export function when(value) {
  if (!value) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : formatDateTime(d);
}

export function ago(value) {
  if (!value) return 'Never';
  const t = new Date(value).getTime();
  if (Number.isNaN(t)) return String(value);
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (s < 60) return 'Just now';
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)} d ago`;
  return when(value);
}

const STATUS = {
  active: ['Active', 'badge-success'],
  expiring: ['Expires soon', 'badge-warning'],
  owner_inactive: ['Owner inactive', 'badge-danger'],
  rotating: ['Rotating', 'badge-warning'],
  revoked: ['Revoked', 'badge-secondary'],
  expired: ['Expired', 'badge-danger'],
  pending_claim: ['Waiting for developer', 'badge-secondary'],
};

export function statusBadge(k) {
  const [label0, cls] = STATUS[k.status] || [k.status || '—', 'badge-secondary'];
  let label = label0;
  if (k.status === 'expiring' && k.expires_at) {
    const days = Math.max(0, Math.ceil((new Date(k.expires_at).getTime() - Date.now()) / 86400000));
    label = `Expires in ${days} d`;
  }
  return `<span class="badge ${cls}">${esc(label)}</span>`;
}

export function chipsOf(k, mods) {
  const name = (key) => (mods.find(m => m.key === key) || { name: key }).name;
  const entries = Object.entries(k.modules || {});
  const legacy = (k.legacy_scopes || []);
  if (!entries.length && !legacy.length) return '<span class="api-muted">None</span>';
  const chips = entries.map(([key, lvl]) => `<span class="api-chip${lvl === 'write' ? ' api-chip-write' : ''}">${esc(name(key))} · ${lvl === 'write' ? 'Change' : 'View'}</span>`);
  legacy.forEach(s => chips.push(`<span class="api-chip">${esc(s)}</span>`));
  return `<div class="api-chips">${chips.join('')}</div>`;
}

// attentionText: the plain line for a failing / expiring key.
export function attentionText(a) {
  if (a.message) return a.message;
  const name = a.name || 'This key';
  const lvl = a.level === 'write' ? 'Change' : 'View';
  if (a.kind === 'failing') return a.module ? `${name} is failing: needs ${lvl} on ${a.module_name || a.module}` : `${name} is failing`;
  if (a.kind === 'expiring') return `${name} expires ${a.expires_at ? when(a.expires_at) : 'soon'}`;
  if (a.kind === 'rotating') return `${name} is rotating: update the new key`;
  if (a.kind === 'owner_inactive') return `${name}: owner is inactive`;
  return name;
}

// attentionHTML renders alert rows; Add (the one-click fix) only where the
// answer names a module and level and the server did not say it cannot.
export function attentionHTML(items) {
  return items.map((a, i) => `<div class="api-alert"><span>${esc(attentionText(a))}</span>
    ${a.module && a.level && a.can_fix !== false && a.kind === 'failing' ? `<button type="button" class="btn btn-primary btn-sm" data-fix="${i}">Add</button>` : ''}</div>`).join('');
}

// bindFix wires the Add buttons; onDone runs after a fix lands.
export function bindFix(host, items, onDone) {
  host.querySelectorAll('[data-fix]').forEach(btn => btn.addEventListener('click', async () => {
    const a = items[Number(btn.dataset.fix)];
    await guardAgainstDoubleSubmit(btn, 'Adding…', async () => {
      const r = await call(`/keys/${encodeURIComponent(a.key_id || a.id)}/fix`, 'POST', { module: a.module, level: a.level });
      if (!r) return;
      if (!r.ok) { await showApiError(r, 'It was not added.'); return; }
      showToast('Added.');
      onDone();
    });
  }));
}

// mountShareDocs: Share docs (link, days <= 90, withdraw), Download OpenAPI /
// Postman and Send to developer for one key. Used on the Created page and the detail page.
export async function mountShareDocs(host, id) {
  host.innerHTML = `<div class="api-row"><div class="form-group"><label class="form-label" for="api-share-days-${esc(id)}">Link days</label>
    <input id="api-share-days-${esc(id)}" class="form-input" type="number" min="1" max="90" value="30"></div></div>
    <div class="api-actions"><button type="button" class="btn btn-outline btn-sm" data-share>Share docs</button>
    <button type="button" class="btn btn-outline btn-sm" data-share-mail>Send to developer</button>
    <button type="button" class="btn btn-outline btn-sm" data-dl="openapi">Download OpenAPI</button>
    <button type="button" class="btn btn-outline btn-sm" data-dl="postman">Download Postman</button></div>
    <div class="api-keybox" data-share-out hidden><span class="api-mono" data-share-url></span>
      <button type="button" class="btn btn-outline btn-sm" data-share-copy>Copy</button>
      <button type="button" class="btn btn-outline btn-sm" data-share-off>Withdraw</button>
      <span class="api-muted" data-share-exp></span></div>`;
  const out = host.querySelector('[data-share-out]');
  const show = (link) => {
    if (!link || !link.url) { out.hidden = true; return; }
    const full = /^https?:/.test(link.url) ? link.url : location.origin + link.url;
    out.hidden = false;
    out.querySelector('[data-share-url]').textContent = full;
    out.querySelector('[data-share-exp]').textContent = link.expires_at ? `Ends ${when(link.expires_at)}` : '';
  };
  const ensure = async () => {
    const days = Math.min(90, Math.max(1, Number(host.querySelector('input[type=number]').value) || 30));
    const r = await call(`/keys/${encodeURIComponent(id)}/share-docs`, 'POST', { days });
    if (!r) return null;
    if (!r.ok) { await showApiError(r, 'The link was not made.'); return null; }
    const link = await r.json();
    show(link);
    return link;
  };
  host.querySelector('[data-share]').addEventListener('click', (e) => guardAgainstDoubleSubmit(e.currentTarget, 'Making…', ensure));
  host.querySelector('[data-share-mail]').addEventListener('click', async (e) => {
    await guardAgainstDoubleSubmit(e.currentTarget, 'Making…', async () => {
      const link = await ensure();
      if (!link || !link.url) return;
      const full = /^https?:/.test(link.url) ? link.url : location.origin + link.url;
      location.href = `mailto:?subject=${encodeURIComponent('API docs')}&body=${encodeURIComponent(`Docs for the API: ${full}`)}`;
    });
  });
  host.querySelector('[data-share-copy]').addEventListener('click', async (e) => {
    const ok = await copyText(out.querySelector('[data-share-url]').textContent);
    showToast(ok ? 'Copied.' : 'Select and copy.');
  });
  host.querySelector('[data-share-off]').addEventListener('click', async () => {
    const r = await call(`/keys/${encodeURIComponent(id)}/share-docs`, 'DELETE');
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The link was not withdrawn.'); return; }
    show(null);
    showToast('Link withdrawn.');
  });
  host.querySelectorAll('[data-dl]').forEach(btn => btn.addEventListener('click', async () => {
    const kind = btn.dataset.dl;
    const r = await call(`/keys/${encodeURIComponent(id)}/${kind}.json`);
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'The file could not be made.'); return; }
    downloadText(kind === 'openapi' ? 'openapi.json' : 'postman.json', await r.text(), 'application/json');
  }));
  const cur = await call(`/keys/${encodeURIComponent(id)}/share-docs`);
  if (cur && cur.ok) show(await cur.json().catch(() => null));
}

// renderKeyDetail: ctx { mods, isAdmin, onChanged(), onClosed(), showCreated(body) }.
export async function renderKeyDetail(host, id, ctx) {
  host.innerHTML = '<p class="api-muted">Loading…</p>';
  const [r, attRes] = await Promise.all([call(`/keys/${encodeURIComponent(id)}`), call('/attention')]);
  if (!r) return;
  if (!r.ok) { host.innerHTML = ''; await showApiError(r, 'The key could not be loaded.'); return; }
  const body = await r.json();
  const k = body.key || body.credential || body;
  const limits = body.limits || k.limits || {};
  const attention = attRes && attRes.ok ? rowsOf(await attRes.json().catch(() => []), 'items', 'attention').filter(a => String(a.key_id || a.id) === String(id)) : [];
  const live = !['revoked', 'expired'].includes(k.status);

  host.innerHTML = `<div class="table-panel api-pad">
    <div data-att>${attentionHTML(attention)}</div>
    <div class="api-bar"><strong>${esc(k.name)}</strong> ${statusBadge(k)}<span class="api-spacer"></span>
      ${live ? '<button type="button" class="btn btn-outline btn-sm" data-act="rotate">Rotate</button>' : ''}
      ${live && ctx.isAdmin ? '<button type="button" class="btn btn-outline btn-sm" data-act="takeover">Take over</button>' : ''}
      ${live ? '<button type="button" class="btn btn-danger btn-sm" data-act="revoke">Revoke</button>' : ''}</div>
    <div data-rotate-row class="api-bar" hidden><label class="form-label" for="api-overlap">Old key works for</label>
      <select id="api-overlap" class="form-select"><option value="now">Now (ends at once)</option><option value="1h">1 hour</option><option value="24h" selected>24 hours</option><option value="7d">7 days</option></select>
      <button type="button" class="btn btn-primary btn-sm" data-act="rotate-go">Rotate now</button></div>
    <div class="api-tabs" role="tablist">${['overview:Overview', 'modules:Modules', 'activity:Activity', 'limits:Limits', 'webhooks:Webhooks', 'share:Share'].map((t, i) => {
      const [v, l] = t.split(':');
      return `<button type="button" class="api-tab${i ? '' : ' active'}" role="tab" data-tab="${v}">${l}</button>`;
    }).join('')}</div>
    <div data-tabbody></div></div>`;
  bindFix(host, attention, () => ctx.onChanged(id));

  const body_ = host.querySelector('[data-tabbody]');
  const tabs = {
    overview() {
      const stores = (k.stores || []).length ? k.stores.join(', ') : 'All stores';
      body_.innerHTML = `<dl class="api-kv">
        <div><dt>Owner</dt><dd>${esc(k.owner_name || k.owner_id || '—')}${k.owner_active === false ? ' <span class="badge badge-danger">Inactive</span>' : ''}</dd></div>
        <div><dt>Sign-in</dt><dd>${k.auth_mode === 'oauth_client' ? 'App login (OAuth)' : 'Secret key'}</dd></div>
        <div><dt>Key</dt><dd class="api-mono">${esc(k.key_prefix || '—')}</dd></div>
        <div><dt>Expires</dt><dd>${k.expires_at ? esc(when(k.expires_at)) : 'Never'}</dd></div>
        ${k.old_key_ends_at ? `<div><dt>Old key ends</dt><dd>${esc(when(k.old_key_ends_at))}</dd></div>` : ''}
        <div><dt>Last use</dt><dd>${esc(ago(k.last_used_at))}</dd></div>
        <div><dt>Calls today</dt><dd>${esc(k.calls_today ?? 0)}</dd></div>
        <div><dt>Errors today</dt><dd>${esc(k.errors_today ?? 0)}</dd></div>
        <div><dt>Stores</dt><dd>${esc(stores)}</dd></div>
        <div><dt>IPs</dt><dd>${(k.ip_allow || []).length ? esc(k.ip_allow.join(', ')) : 'Any'}</dd></div>
        <div><dt>Modules</dt><dd>${chipsOf(k, ctx.mods)}</dd></div></dl>`;
    },
    modules() {
      body_.innerHTML = '<div data-grid></div><div class="api-actions"><button type="button" class="btn btn-primary" data-save-mods>Save</button></div>';
      const grid = buildModuleGrid(body_.querySelector('[data-grid]'), ctx.mods, { modules: k.modules, doctypes: k.doctypes, can_approve: k.can_approve });
      body_.querySelector('[data-save-mods]').addEventListener('click', (e) => guardAgainstDoubleSubmit(e.currentTarget, 'Saving…', async () => {
        const rr = await call(`/keys/${encodeURIComponent(id)}/modules`, 'PUT', grid.read());
        if (!rr) return;
        if (!rr.ok) { await showApiError(rr, 'The modules were not saved.'); return; }
        showToast('Saved.');
        ctx.onChanged(id);
      }));
    },
    async activity() {
      body_.innerHTML = '<label class="api-check"><input type="checkbox" data-errs> Errors only</label><div data-act-out><p class="api-muted">Loading…</p></div>';
      const load = async () => {
        const errs = body_.querySelector('[data-errs]').checked;
        const out = body_.querySelector('[data-act-out]');
        const rr = await call(`/keys/${encodeURIComponent(id)}/activity?limit=50${errs ? '&errors_only=1' : ''}`);
        if (!rr) return;
        if (!rr.ok) { out.innerHTML = ''; await showApiError(rr, 'Activity could not be loaded.'); return; }
        const rows = rowsOf(await rr.json(), 'calls', 'entries', 'items');
        out.innerHTML = rows.length === 0 ? '<p class="api-muted">No calls.</p>'
          : `<div class="table-wrapper"><table><thead><tr><th>When</th><th>Call</th><th>Status</th><th>Result</th></tr></thead><tbody>${rows.map(e => `<tr>
            <td>${esc(when(e.at || e.created_at))}</td><td class="api-mono">${esc(`${e.method} ${e.path}`)}</td><td>${esc(e.status ?? e.status_code)}</td>
            <td>${esc(e.code || e.outcome || '')}${e.fix ? `<div class="api-why api-muted">${esc(e.fix)}</div>` : ''}</td></tr>`).join('')}</tbody></table></div>`;
      };
      body_.querySelector('[data-errs]').addEventListener('change', load);
      await load();
    },
    limits() {
      body_.innerHTML = `<div class="api-row"><div class="form-group"><label class="form-label" for="api-lim-min">Calls a minute</label>
        <input id="api-lim-min" class="form-input" type="number" min="1" value="${esc(limits.rate_limit_per_minute ?? '')}"></div>
        <div class="form-group"><label class="form-label" for="api-lim-day">Calls a day</label>
        <input id="api-lim-day" class="form-input" type="number" min="1" value="${esc(limits.daily_quota ?? '')}"></div></div>
        <button type="button" class="btn btn-primary" data-save-lim>Save</button>`;
      body_.querySelector('[data-save-lim]').addEventListener('click', (e) => guardAgainstDoubleSubmit(e.currentTarget, 'Saving…', async () => {
        const num = (sel) => { const v = body_.querySelector(sel).value.trim(); return v === '' ? null : Number(v); };
        const rr = await call(`/keys/${encodeURIComponent(id)}/limits`, 'PUT', { rate_limit_per_minute: num('#api-lim-min'), daily_quota: num('#api-lim-day') });
        if (!rr) return;
        if (!rr.ok) { await showApiError(rr, 'The limits were not saved.'); return; }
        showToast('Saved.');
      }));
    },
    async webhooks() {
      body_.innerHTML = `<div class="api-row"><div class="form-group"><label class="form-label" for="api-wh-url">Send to (https)</label>
        <input id="api-wh-url" class="form-input" placeholder="https://example.com/hook"></div>
        <div class="form-group"><label class="form-label" for="api-wh-ev">Events (blank = all)</label>
        <input id="api-wh-ev" class="form-input" placeholder="procurement.PurchaseOrder.submitted"></div>
        <button type="button" class="btn btn-primary" data-wh-add>Add</button></div>
        <div data-wh-subs></div><div class="api-h2">Deliveries</div><div data-wh-log></div>`;
      const paint = async () => {
        const [sr, dr] = await Promise.all([call(`/keys/${encodeURIComponent(id)}/webhooks`), call(`/keys/${encodeURIComponent(id)}/webhooks/deliveries`)]);
        const subs = sr && sr.ok ? rowsOf(await sr.json().catch(() => []), 'subscriptions', 'webhooks', 'items') : [];
        const dels = dr && dr.ok ? rowsOf(await dr.json().catch(() => []), 'deliveries', 'items') : [];
        body_.querySelector('[data-wh-subs]').innerHTML = subs.length === 0 ? '<p class="api-muted">No webhooks.</p>'
          : `<div class="table-wrapper"><table><thead><tr><th>URL</th><th>Events</th><th></th></tr></thead><tbody>${subs.map(s => `<tr>
            <td class="api-mono">${esc(s.url)}</td><td>${esc((s.events || []).length ? s.events.join(', ') : 'All')}</td>
            <td><button type="button" class="action-btn action-btn-danger" data-wh-del="${esc(s.id)}">Remove</button></td></tr>`).join('')}</tbody></table></div>`;
        body_.querySelector('[data-wh-log]').innerHTML = dels.length === 0 ? '<p class="api-muted">No deliveries.</p>'
          : `<div class="table-wrapper"><table><thead><tr><th>When</th><th>Event</th><th>Result</th><th>Tries</th><th></th></tr></thead><tbody>${dels.map(d => `<tr>
            <td>${esc(when(d.created_at || d.at))}</td><td class="api-mono">${esc(d.event)}</td>
            <td>${esc(d.status || '')}${d.response_code ? ` ${esc(d.response_code)}` : ''}${d.last_error ? `<div class="api-muted">${esc(d.last_error)}</div>` : ''}</td>
            <td>${esc(d.attempts ?? '')}</td>
            <td><button type="button" class="action-btn" data-wh-resend="${esc(d.id)}">Resend</button></td></tr>`).join('')}</tbody></table></div>`;
      };
      body_.querySelector('[data-wh-add]').addEventListener('click', (e) => guardAgainstDoubleSubmit(e.currentTarget, 'Adding…', async () => {
        const url = body_.querySelector('#api-wh-url').value.trim();
        if (!url) { showToast('Add the address.'); return; }
        const events = body_.querySelector('#api-wh-ev').value.split(/[,\s]+/).filter(Boolean);
        const rr = await call(`/keys/${encodeURIComponent(id)}/webhooks`, 'POST', { url, events });
        if (!rr) return;
        if (!rr.ok) { await showApiError(rr, 'The webhook was not added.'); return; }
        const made = await rr.json().catch(() => ({}));
        if (made.secret && window.showOneTimeSecretDialog) await window.showOneTimeSecretDialog('Webhook added', 'Signing secret, shown once:', made.secret);
        body_.querySelector('#api-wh-url').value = '';
        body_.querySelector('#api-wh-ev').value = '';
        await paint();
      }));
      // onclick, not addEventListener: body_ outlives the tab, and each visit
      // added another listener, so one Remove or Resend went out twice (ask 226).
      body_.onclick = async (e) => {
        const rm = e.target.closest('[data-wh-del]');
        const rs = e.target.closest('[data-wh-resend]');
        if (rm) {
          const rr = await call(`/keys/${encodeURIComponent(id)}/webhooks/${encodeURIComponent(rm.dataset.whDel)}`, 'DELETE');
          if (!rr) return;
          if (!rr.ok) { await showApiError(rr, 'The webhook was not removed.'); return; }
          await paint();
        } else if (rs) {
          const rr = await call(`/keys/${encodeURIComponent(id)}/webhooks/deliveries/${encodeURIComponent(rs.dataset.whResend)}/resend`, 'POST', {});
          if (!rr) return;
          if (!rr.ok) { await showApiError(rr, 'It was not resent.'); return; }
          showToast('Resent.');
          await paint();
        }
      };
      await paint();
    },
    async share() {
      body_.innerHTML = '<div data-share-host></div>';
      await mountShareDocs(body_.querySelector('[data-share-host]'), id);
    },
  };
  host.querySelectorAll('[data-tab]').forEach(t => t.addEventListener('click', () => {
    host.querySelectorAll('[data-tab]').forEach(x => x.classList.toggle('active', x === t));
    tabs[t.dataset.tab]();
  }));
  tabs.overview();

  host.querySelector('[data-act="rotate"]')?.addEventListener('click', () => { const row = host.querySelector('[data-rotate-row]'); row.hidden = !row.hidden; });
  host.querySelector('[data-act="rotate-go"]').addEventListener('click', (e) => guardAgainstDoubleSubmit(e.currentTarget, 'Rotating…', async () => {
    const overlap = host.querySelector('#api-overlap').value;
    const rr = await call(`/keys/${encodeURIComponent(id)}/rotate`, 'POST', { overlap });
    if (!rr) return;
    if (!rr.ok) { await showApiError(rr, 'The key was not rotated.'); return; }
    ctx.showCreated(await rr.json(), { id, rotated: true });
  }));
  host.querySelector('[data-act="revoke"]')?.addEventListener('click', async () => {
    if (!await showCustomConfirm('Revoke this key? Every call using it is refused from now on. This cannot be undone.')) return;
    const rr = await call(`/keys/${encodeURIComponent(id)}`, 'DELETE');
    if (!rr) return;
    if (!rr.ok) { await showApiError(rr, 'The key was not revoked.'); return; }
    showToast('Revoked.');
    ctx.onClosed();
  });
  host.querySelector('[data-act="takeover"]')?.addEventListener('click', async () => {
    if (!await showCustomConfirm('Take over this key? You become its owner and it runs with your rights.')) return;
    const rr = await call(`/keys/${encodeURIComponent(id)}/take-over`, 'POST', {});
    if (!rr) return;
    if (!rr.ok) { await showApiError(rr, 'The key was not taken over.'); return; }
    showToast('You are the owner now.');
    ctx.onChanged(id);
  });
}
