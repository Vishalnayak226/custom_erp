// Stage 47.12 / 49.17.3: Settings -> Configuration -> Go-live. Five pages:
// scope questions, the structure graph, the finance start, the cutover pack and
// the go-live gate. Server: internal/server/handlers_golive.go. It is a record
// and a sign-off; nothing else in the ERP reads it.

const GL = '/api/v1/golive';
const gEsc = s => escapeHTMLText(String(s == null ? '' : s));
const STATE_BADGE = {
  'Pass': 'badge-success', 'Fail': 'badge-danger', 'Not evidenced': 'badge-warning', 'Waived': 'badge-warning', 'Not applicable': 'badge-secondary',
  'Supported': 'badge-success', 'Preview': 'badge-warning', 'Unsupported': 'badge-danger', 'Open': 'badge-secondary'
};
const badge = s => `<span class="badge ${STATE_BADGE[s] || 'badge-secondary'}">${gEsc(s)}</span>`;

async function gGet(path) {
  const res = await apiFetch(GL + path);
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'It could not be loaded.'); return null; }
  return res.json();
}

async function gPost(path, body, okMsg) {
  const res = await apiFetch(GL + path, { method: 'POST', body: JSON.stringify(body || {}) });
  if (!res) return null;
  if (!res.ok) { await showApiError(res, 'It could not be saved.'); return null; }
  if (okMsg) showToast(okMsg);
  return res.json();
}

function gFix(fix) {
  if (!fix) return;
  if (fix.startsWith('doctype:')) { window.openSetupDoctype(fix.slice(8)); return; }
  if (fix.startsWith('configuration:')) { renderView(fix); return; }
  setActiveMenu(`menu-${fix}`);
  renderView(fix);
}

function gTitle(text) {
  return `<h3 class="s-afd9a1">${gEsc(text)}</h3>`;
}

// ---- scope ------------------------------------------------------------

export async function renderGoLiveScope(host) {
  host.innerHTML = gTitle('Go-live scope') + '<p class="text-muted">Loading…</p>';
  const s = await gGet('/scope');
  if (!s) { host.innerHTML = gTitle('Go-live scope') + '<p class="text-muted">Not available.</p>'; return; }
  const draw = scope => {
    const answers = scope.answers || {};
    const qs = scope.questions.map(q => `
      <fieldset class="s-9c1de5">
        <legend class="s-3d63a5">${gEsc(q.label)}</legend>
        <div class="s-ed526d">${q.options.map(o => `
          <label class="s-5ef167">
            <input type="${q.multi ? 'checkbox' : 'radio'}" name="gl-${gEsc(q.id)}" value="${gEsc(o.value)}" data-gl-q="${gEsc(q.id)}"${(answers[q.id] || []).includes(o.value) ? ' checked' : ''}>
            ${gEsc(o.label)}</label>`).join('')}
        </div>
      </fieldset>`).join('');
    const choices = (scope.choices || []).map(c => `<tr><td>${gEsc(c.label)}</td><td>${badge(c.support)}</td><td>${gEsc(c.level || '')}</td><td class="text-muted">${gEsc(c.detail || '')}</td></tr>`).join('');
    const modules = (scope.modules || []).map(m => `<tr><td>${gEsc(m.module)}</td><td>${badge(m.level)}</td><td class="text-muted">${gEsc(m.titles)}</td></tr>`).join('');
    host.innerHTML = gTitle('Go-live scope') + `
      <div class="s-0d3d73">${qs}
        <div class="s-b480d0">
          <button type="button" class="btn btn-primary" data-gl="save">Save</button>
          <span class="text-muted" data-gl="saved">${scope.saved ? `Version ${gEsc(scope.version)} · ${gEsc(scope.saved_by)} · ${gEsc(scope.saved_at)}` : 'Not saved'}</span>
        </div>
        ${scope.saved ? `<div class="table-panel s-2b583d"><table class="data-table"><thead><tr><th>Choice</th><th>Support</th><th>Level</th><th></th></tr></thead><tbody>${choices}</tbody></table></div>
        ${modules ? `<div class="table-panel"><table class="data-table"><thead><tr><th>Module</th><th>Level</th><th>Needs</th></tr></thead><tbody>${modules}</tbody></table></div>` : ''}` : ''}
      </div>`;
    host.querySelector('[data-gl="save"]').addEventListener('click', async e => {
      const btn = e.currentTarget;
      const out = {};
      host.querySelectorAll('[data-gl-q]:checked').forEach(i => { (out[i.dataset.glQ] = out[i.dataset.glQ] || []).push(i.value); });
      btn.disabled = true;
      const r = await gPost('/scope', { answers: out }, 'Saved');
      if (r) draw(r); else btn.disabled = false;
    });
  };
  draw(s);
}

// ---- structure graph --------------------------------------------------

const GRAPH_COLS = [['entity', 'Legal entity'], ['registration', 'Registration'], ['location', 'Location'], ['warehouse', 'Warehouse'], ['dimension', 'Dimension']];
const NODE_FILL = { ok: '#e6f4ea', issue: '#fdecea', out: '#eceff1', expected: '#fff8e1' };
const NODE_STROKE = { ok: '#2e7d32', issue: '#c62828', out: '#90a4ae', expected: '#f9a825' };

export async function renderGoLiveGraph(host) {
  host.innerHTML = gTitle('Structure') + '<p class="text-muted">Loading…</p>';
  let selected = '';
  const load = async () => {
    const g = await gGet('/graph');
    if (!g) { host.innerHTML = gTitle('Structure') + '<p class="text-muted">Not available.</p>'; return; }
    const W = 190, H = 46, GX = 36, GY = 18, PAD = 12;
    const pos = {};
    const colMax = [];
    GRAPH_COLS.forEach(([kind], ci) => {
      const ns = g.nodes.filter(n => n.kind === kind);
      ns.forEach((n, ri) => { pos[n.id] = { x: PAD + ci * (W + GX), y: 30 + PAD + ri * (H + GY) }; });
      colMax.push(ns.length);
    });
    const height = 30 + PAD * 2 + Math.max(1, ...colMax) * (H + GY);
    const width = PAD * 2 + GRAPH_COLS.length * W + (GRAPH_COLS.length - 1) * GX;
    const edges = g.edges.filter(e => pos[e.from] && pos[e.to]).map(e => {
      const a = pos[e.from], b = pos[e.to];
      return `<path d="M${a.x + W} ${a.y + H / 2} C${a.x + W + GX / 2} ${a.y + H / 2} ${b.x - GX / 2} ${b.y + H / 2} ${b.x} ${b.y + H / 2}" fill="none" stroke="#90a4ae" stroke-width="1.5"/>`;
    }).join('');
    const heads = GRAPH_COLS.map(([, label], ci) => `<text x="${PAD + ci * (W + GX)}" y="20" font-size="12" fill="#546e7a">${gEsc(label)}</text>`).join('');
    const nodes = g.nodes.map(n => {
      const p = pos[n.id];
      const dash = n.state === 'expected' ? ' stroke-dasharray="5 3"' : '';
      return `<g data-gl-node="${gEsc(n.id)}" class="s-3b6a3a" tabindex="0" role="button" aria-label="${gEsc(n.label)} ${gEsc(n.state)}">
        <rect x="${p.x}" y="${p.y}" width="${W}" height="${H}" rx="6" fill="${NODE_FILL[n.state]}" stroke="${NODE_STROKE[n.state]}" stroke-width="${n.id === selected ? 3 : 1.5}"${dash}/>
        <text x="${p.x + 10}" y="${p.y + 20}" font-size="13" font-weight="600" fill="#263238">${gEsc(n.label.slice(0, 24))}</text>
        <text x="${p.x + 10}" y="${p.y + 36}" font-size="11" fill="#546e7a">${gEsc(n.state === 'ok' ? 'OK' : n.state === 'out' ? 'Out of scope' : n.state === 'expected' ? 'Missing' : (n.issues[0] || 'Check').slice(0, 30))}</text>
      </g>`;
    }).join('');
    host.innerHTML = gTitle('Structure') + `
      <div class="s-00116b">
        ${['ok', 'issue', 'expected', 'out'].map(k => `<span class="badge">${gEsc({ ok: 'OK', issue: 'To fix', expected: 'Missing', out: 'Out of scope' }[k])} ${gEsc(g.counts[k] || 0)}</span>`).join('')}
      </div>
      <div class="table-panel s-75426d"><svg width="${width}" height="${height}" role="img" aria-label="Structure">${heads}${edges}${nodes}</svg></div>
      <div data-gl="detail" class="s-371498"></div>`;
    const detail = host.querySelector('[data-gl="detail"]');
    const showNode = id => {
      const n = g.nodes.find(x => x.id === id);
      if (!n) return;
      selected = id;
      host.querySelectorAll('[data-gl-node] rect').forEach(r => r.setAttribute('stroke-width', '1.5'));
      const rect = host.querySelector(`[data-gl-node="${CSS.escape(id)}"] rect`);
      if (rect) rect.setAttribute('stroke-width', '3');
      detail.innerHTML = `<div class="table-panel s-8ebc43">
        <strong>${gEsc(n.label)}</strong>
        <ul class="s-1c2ae9">${n.issues.map(i => `<li>${gEsc(i)}</li>`).join('') || '<li>No problem</li>'}</ul>
        ${n.example ? `<div class="text-muted">For example: ${gEsc(n.example)}</div>` : ''}
        ${n.state === 'out' ? `<div class="text-muted">Out of scope: ${gEsc(n.out_by)} · ${gEsc(n.out_at)} · ${gEsc(n.reason)}</div>` : ''}
        <div class="s-c441ff">
          ${n.fix ? '<button type="button" class="btn btn-outline" data-gl="fix">Open</button>' : ''}
          ${n.doc && n.state !== 'out' ? '<input class="form-control s-55585d" maxlength="1000" placeholder="Reason" data-gl="reason"><button type="button" class="btn btn-outline" data-gl="out">Out of scope</button>' : ''}
          ${n.doc && n.state === 'out' ? '<button type="button" class="btn btn-outline" data-gl="in">Bring back</button>' : ''}
        </div></div>`;
      const fix = detail.querySelector('[data-gl="fix"]');
      if (fix) fix.addEventListener('click', () => gFix(n.fix));
      const setScope = async out => {
        const reason = out ? detail.querySelector('[data-gl="reason"]').value : '';
        if (await gPost('/graph/scope', { doc: n.doc, out, reason }, out ? 'Set out of scope' : 'Back in scope')) load();
      };
      const o = detail.querySelector('[data-gl="out"]'); if (o) o.addEventListener('click', () => setScope(true));
      const i = detail.querySelector('[data-gl="in"]'); if (i) i.addEventListener('click', () => setScope(false));
    };
    host.querySelectorAll('[data-gl-node]').forEach(el => {
      el.addEventListener('click', () => showNode(el.dataset.glNode));
      el.addEventListener('keydown', e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); showNode(el.dataset.glNode); } });
    });
    if (selected) showNode(selected);
  };
  load();
}

// ---- packs ------------------------------------------------------------

export async function renderGoLivePack(host, pack) {
  host.innerHTML = '<p class="text-muted">Loading…</p>';
  const p = await gGet(`/packs/${encodeURIComponent(pack)}`);
  if (!p) { host.innerHTML = '<p class="text-muted">Not available.</p>'; return; }
  const counts = Object.entries(p.counts).map(([k, v]) => `${badge(k)} ${gEsc(v)}`).join(' &nbsp; ');
  const rows = p.lines.map(l => `
    <tr data-gl-line="${gEsc(l.id)}">
      <td><strong>${gEsc(l.label)}</strong> ${l.severity === 'Critical' ? '<span class="badge badge-danger">Critical</span>' : ''}
        <div class="text-muted s-6750b2">${gEsc(l.detail)}</div>
        <div class="text-muted s-6cb285">${gEsc(l.source)}${l.evidence ? ` · ${gEsc(l.evidence.by)} ${gEsc(l.evidence.at)}` : ''}${l.waiver ? ` · waived by ${gEsc(l.waiver.by)} until ${gEsc(l.waiver.expires_on)}: ${gEsc(l.waiver.reason)}${l.waiver.active ? '' : ' (expired)'}` : ''}</div>
        <div data-gl-form="${gEsc(l.id)}"></div></td>
      <td>${badge(l.state)}</td>
      <td class="s-86abda">
        ${l.fix ? `<button type="button" class="action-btn" data-gl-fix="${gEsc(l.fix)}">Open</button>` : ''}
        ${l.manual && l.state !== 'Not applicable' ? `<button type="button" class="action-btn" data-gl-ev="${gEsc(l.id)}">Evidence</button>` : ''}
        ${(l.state === 'Fail' || l.state === 'Not evidenced') ? `<button type="button" class="action-btn" data-gl-wv="${gEsc(l.id)}">Waive</button>` : ''}
        ${l.state === 'Waived' ? `<button type="button" class="action-btn" data-gl-end="${gEsc(l.id)}">End waiver</button>` : ''}
      </td>
    </tr>`).join('');
  host.innerHTML = gTitle(p.title) + `
    <div class="s-761d3a">${counts}</div>
    <div class="table-panel s-18adbe"><table class="data-table"><thead><tr><th>Check</th><th>State</th><th></th></tr></thead><tbody>${rows}</tbody></table></div>
    <div class="table-panel s-758a28">
      ${p.sign_off ? `<div class="s-fdf33f"><strong>Signed</strong> · ${gEsc(p.sign_off.by)} · ${gEsc(p.sign_off.at)} ${gEsc(p.sign_off.note)}${p.sign_off.reopened ? ' · Reopened' : ''}</div>` : ''}
      <div class="s-73d754">
        <input class="form-control s-4a724c" maxlength="2000" placeholder="Note" data-gl="so-note">
        <button type="button" class="btn btn-primary" data-gl="signoff">Sign off</button>
        ${p.open ? `<span class="text-muted">${gEsc(p.open)} open</span>` : ''}
      </div>
    </div>`;
  const again = () => renderGoLivePack(host, pack);
  const form = (id, html) => { host.querySelector(`[data-gl-form="${CSS.escape(id)}"]`).innerHTML = `<div class="s-6e866a">${html}</div>`; };
  host.querySelectorAll('[data-gl-fix]').forEach(b => b.addEventListener('click', () => gFix(b.dataset.glFix)));
  host.querySelectorAll('[data-gl-ev]').forEach(b => b.addEventListener('click', () => {
    const id = b.dataset.glEv;
    form(id, `<input class="form-control s-aef3df" maxlength="2000" placeholder="Note" data-gl="note">
      <input class="form-control s-a5d7a0" maxlength="500" placeholder="Reference" data-gl="ref">
      <button type="button" class="btn btn-primary btn-sm" data-gl="attach">Attach</button>`);
    const f = host.querySelector(`[data-gl-form="${CSS.escape(id)}"]`);
    f.querySelector('[data-gl="attach"]').addEventListener('click', async () => {
      const r = await gPost(`/packs/${pack}/evidence`, { line: id, note: f.querySelector('[data-gl="note"]').value, reference: f.querySelector('[data-gl="ref"]').value }, 'Attached');
      if (r) again();
    });
  }));
  host.querySelectorAll('[data-gl-wv]').forEach(b => b.addEventListener('click', () => {
    const id = b.dataset.glWv;
    form(id, `<input class="form-control s-aef3df" maxlength="1000" placeholder="Reason" data-gl="reason">
      <input type="date" class="form-control s-132d31" data-gl="exp" aria-label="Until">
      <button type="button" class="btn btn-primary btn-sm" data-gl="waive">Waive</button>`);
    const f = host.querySelector(`[data-gl-form="${CSS.escape(id)}"]`);
    f.querySelector('[data-gl="waive"]').addEventListener('click', async () => {
      const r = await gPost(`/packs/${pack}/waive`, { line: id, reason: f.querySelector('[data-gl="reason"]').value, expires_on: f.querySelector('[data-gl="exp"]').value }, 'Waived');
      if (r) again();
    });
  }));
  host.querySelectorAll('[data-gl-end]').forEach(b => b.addEventListener('click', async () => {
    if (await gPost(`/packs/${pack}/waive/end`, { line: b.dataset.glEnd }, 'Waiver ended')) again();
  }));
  host.querySelector('[data-gl="signoff"]').addEventListener('click', async e => {
    const btn = e.currentTarget;
    btn.disabled = true;
    const r = await gPost(`/packs/${pack}/signoff`, { note: host.querySelector('[data-gl="so-note"]').value }, 'Signed');
    if (r) again(); else btn.disabled = false;
  });
}
