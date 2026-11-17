// 205.18 / 205.19: the API docs viewer. Our own small plain-JS reader for the
// generated OpenAPI spec (no Swagger or Redoc, Article 1). Shared by the
// Settings screen (view-api-access.js) and the public shared-docs page
// (api-docs.html), so it holds no app globals: nothing here needs a login.

export function esc(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch (_) {
    const t = document.createElement('textarea');
    t.value = text;
    document.body.appendChild(t);
    t.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_e) { ok = false; }
    t.remove();
    return ok;
  }
}

export function downloadText(name, text, type = 'text/plain') {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// snippets: the three copy-ready calls. opts: base, tenant, key, method, path, body.
export function snippets({ base, tenant = '', key = 'YOUR_API_KEY', method = 'GET', path = '/me', body = null }) {
  const url = /^https?:/.test(path) ? path : `${base}${path}`;
  const write = method !== 'GET' && method !== 'DELETE';
  const json = body == null ? '' : JSON.stringify(body, null, 2);
  const hdr = [['Authorization', `Bearer ${key}`]];
  if (tenant) hdr.push(['X-Tenant-ID', tenant]);
  if (write) hdr.push(['Content-Type', 'application/json'], ['Idempotency-Key', 'a-unique-id-per-write']);
  const curl = [`curl -X ${method} "${url}"`, ...hdr.map(([k, v]) => `  -H "${k}: ${v}"`)];
  if (write && json) curl.push(`  -d '${json.replace(/'/g, "'\\''")}'`);
  const js = `const res = await fetch("${url}", {\n  method: "${method}",\n  headers: {\n${hdr.map(([k, v]) => `    "${k}": "${v}"`).join(',\n')}\n  }${write && json ? `,\n  body: JSON.stringify(${json.replace(/\n/g, '\n  ')})` : ''}\n});\nconsole.log(await res.json());`;
  const py = `import requests\n\nres = requests.request(\n    "${method}",\n    "${url}",\n    headers={\n${hdr.map(([k, v]) => `        "${k}": "${v}"`).join(',\n')}\n    }${write && json ? `,\n    json=${json.replace(/\n/g, '\n    ')}` : ''},\n)\nprint(res.json())`;
  return { curl: curl.join(' \\\n'), javascript: js, python: py };
}

function resolveRef(spec, node) {
  let n = node;
  for (let i = 0; n && n.$ref && i < 6; i++) {
    n = String(n.$ref).replace(/^#\//, '').split('/').reduce((o, k) => (o ? o[k] : undefined), spec);
  }
  return n || {};
}

// exampleOf builds a sample value from a JSON schema (or its own example).
export function exampleOf(spec, schema, depth = 0) {
  const s = resolveRef(spec, schema);
  if (s.example !== undefined) return s.example;
  if (s.default !== undefined) return s.default;
  if (s.enum && s.enum.length) return s.enum[0];
  if (depth > 4) return null;
  if (s.type === 'array') return [exampleOf(spec, s.items || {}, depth + 1)];
  if (s.type === 'object' || s.properties) {
    const o = {};
    Object.entries(s.properties || {}).slice(0, 12).forEach(([k, v]) => { o[k] = exampleOf(spec, v, depth + 1); });
    return o;
  }
  if (s.type === 'integer' || s.type === 'number') return 0;
  if (s.type === 'boolean') return false;
  if (s.format === 'date-time') return '2026-01-01T00:00:00Z';
  if (s.format === 'date') return '2026-01-01';
  return 'string';
}

function bodyExample(spec, content) {
  const c = content && (content['application/json'] || Object.values(content)[0]);
  if (!c) return null;
  if (c.example !== undefined) return c.example;
  const ex = c.examples && Object.values(c.examples)[0];
  if (ex && ex.value !== undefined) return ex.value;
  return c.schema ? exampleOf(spec, c.schema) : null;
}

// groups: tag (module) -> [{method, path, op}]
export function groupEndpoints(spec) {
  const groups = new Map();
  Object.entries(spec.paths || {}).forEach(([path, item]) => {
    ['get', 'post', 'put', 'patch', 'delete'].forEach(verb => {
      const op = item[verb];
      if (!op) return;
      const tag = (op.tags && op.tags[0]) || 'General';
      if (!groups.has(tag)) groups.set(tag, []);
      groups.get(tag).push({ method: verb.toUpperCase(), path, op, shared: item.parameters || [] });
    });
  });
  return groups;
}

const GLOSSARY = [
  ['API key', 'A secret that lets another system call this one. Keep it private.'],
  ['Module', 'A part of the ERP, such as Inventory or Orders. A key opens only the modules ticked for it.'],
  ['View', 'The key can read records but not change them.'],
  ['View & change', 'The key can also create and update records.'],
  ['Record', 'One saved item, such as an order or a product.'],
  ['Draft', 'A record that is saved but not final. Keys can make drafts; people approve them.'],
  ['Idempotency-Key', 'A unique id you send with every write. A retry with the same id makes one record, not two.'],
  ['Webhook', 'A message we send to your system when a record changes.'],
  ['Rate limit', 'How many calls a key may make in a minute and a day.'],
];

function startPage(base, tenant) {
  const s = snippets({ base, tenant, path: '/me' });
  return `<h2>Getting started</h2>
    <ol>
      <li>Get an API key from the person who runs your ERP.</li>
      <li>Send it as <span class="api-mono">Authorization: Bearer &lt;key&gt;</span>.</li>
      <li>Call <span class="api-mono">GET ${esc(base)}/me</span> to check it works.</li>
      <li>Open a module on the left and copy a call.</li>
      <li>Add an <span class="api-mono">Idempotency-Key</span> header to every write.</li>
    </ol>
    <h3>First call</h3>${codeBlock(s.curl)}
    <h3>Words used here</h3>
    <dl>${GLOSSARY.map(([t, d]) => `<dt>${esc(t)}</dt><dd>${esc(d)}</dd>`).join('')}</dl>`;
}

function codeBlock(text) {
  return `<div class="api-code"><pre>${esc(text)}</pre><button type="button" class="btn btn-outline btn-sm" data-copy>Copy</button></div>`;
}

function endpointHTML(spec, ep, ctx) {
  const { method, path, op, shared } = ep;
  const params = [...shared, ...(op.parameters || [])].map(p => resolveRef(spec, p));
  const reqBody = op.requestBody ? resolveRef(spec, op.requestBody) : null;
  const reqEx = reqBody ? bodyExample(spec, reqBody.content) : null;
  const responses = Object.entries(op.responses || {}).map(([code, r]) => [code, resolveRef(spec, r)]);
  const okResp = responses.find(([c]) => /^2/.test(c));
  const resEx = okResp ? bodyExample(spec, okResp[1].content) : null;
  const errors = (op['x-errors'] || []).slice();
  responses.filter(([c]) => !/^2/.test(c)).forEach(([c, r]) => {
    if (!errors.some(e => String(e.status) === c)) errors.push({ status: c, cause: r.description || '', fix: r['x-fix'] || '' });
  });
  const sn = snippets({ base: ctx.base, tenant: ctx.tenant, key: ctx.key, method, path: /^\/api\//.test(path) ? ctx.origin + path : path, body: reqEx });
  return `<details class="apidoc-ep"><summary><span class="apidoc-verb">${esc(method)}</span><span class="apidoc-path">${esc(path)}</span>
      <span class="api-muted">${esc(op.summary || '')}</span></summary>
    <div class="apidoc-ep-body">
      ${op.description ? `<p>${esc(op.description)}</p>` : ''}
      ${params.length ? `<h3>Parameters</h3><table class="apidoc-table"><thead><tr><th>Name</th><th>In</th><th>Type</th><th>Note</th></tr></thead><tbody>${params.map(p => `<tr><td class="api-mono">${esc(p.name)}${p.required ? ' *' : ''}</td><td>${esc(p.in)}</td><td>${esc((p.schema && p.schema.type) || '')}</td><td>${esc(p.description || '')}</td></tr>`).join('')}</tbody></table>` : ''}
      ${reqEx != null ? `<h3>Example request body</h3>${codeBlock(JSON.stringify(reqEx, null, 2))}` : ''}
      ${resEx != null ? `<h3>Example response${okResp ? ' ' + esc(okResp[0]) : ''}</h3>${codeBlock(JSON.stringify(resEx, null, 2))}` : ''}
      <h3>Copy a call</h3>
      <div class="api-tabs" data-snip-tabs>${['curl', 'javascript', 'python'].map((k, i) => `<button type="button" class="api-tab${i ? '' : ' active'}" data-snip="${k}">${k === 'javascript' ? 'JavaScript' : k === 'python' ? 'Python' : 'curl'}</button>`).join('')}</div>
      ${['curl', 'javascript', 'python'].map((k, i) => `<div data-snip-pane="${k}"${i ? ' hidden' : ''}>${codeBlock(sn[k])}</div>`).join('')}
      ${errors.length ? `<h3>Errors and fixes</h3><table class="apidoc-table"><thead><tr><th>Code</th><th>Cause</th><th>Fix</th></tr></thead><tbody>${errors.map(e => `<tr><td class="api-mono">${esc(e.code || e.status)}</td><td>${esc(e.cause || e.message || '')}</td><td>${esc(e.fix || '')}</td></tr>`).join('')}</tbody></table>` : ''}
    </div></details>`;
}

// renderApiDocs paints the viewer into host. opts: tenant, key (a real key
// fills the snippets; none leaves a placeholder).
export function renderApiDocs(host, spec, opts = {}) {
  const origin = location.origin;
  const srv = spec.servers && spec.servers[0] && spec.servers[0].url;
  const base = (srv ? (/^https?:/.test(srv) ? srv : origin + srv) : `${origin}/api/public/v1`).replace(/\/$/, '');
  const ctx = { base, origin, tenant: opts.tenant || '', key: opts.key || 'YOUR_API_KEY' };
  const groups = groupEndpoints(spec);
  const titles = ['Getting started', ...groups.keys()];
  host.innerHTML = `<div class="apidoc"><nav class="apidoc-nav" aria-label="Modules">${titles.map((t, i) =>
    `<button type="button" data-doc-nav="${i}" class="${i ? '' : 'active'}">${esc(t)}</button>`).join('')}</nav><div class="apidoc-main"></div></div>`;
  const main = host.querySelector('.apidoc-main');
  const show = (i) => {
    host.querySelectorAll('[data-doc-nav]').forEach(b => b.classList.toggle('active', b.dataset.docNav === String(i)));
    if (i === 0) { main.innerHTML = startPage(base, ctx.tenant); return; }
    const tag = titles[i];
    const t = (spec.tags || []).find(x => x.name === tag);
    main.innerHTML = `<h2>${esc(tag)}</h2>${t && t.description ? `<p>${esc(t.description)}</p>` : ''}${groups.get(tag).map(ep => endpointHTML(spec, ep, ctx)).join('')}`;
  };
  host.addEventListener('click', async (e) => {
    const nav = e.target.closest('[data-doc-nav]');
    if (nav) { show(Number(nav.dataset.docNav)); return; }
    const cp = e.target.closest('[data-copy]');
    if (cp) {
      const ok = await copyText(cp.parentElement.querySelector('pre').textContent);
      cp.textContent = ok ? 'Copied' : 'Select and copy';
      setTimeout(() => { cp.textContent = 'Copy'; }, 1500);
      return;
    }
    const tab = e.target.closest('[data-snip]');
    if (tab) {
      const body = tab.closest('.apidoc-ep-body');
      body.querySelectorAll('[data-snip]').forEach(b => b.classList.toggle('active', b === tab));
      body.querySelectorAll('[data-snip-pane]').forEach(p => { p.hidden = p.dataset.snipPane !== tab.dataset.snip; });
    }
  });
  show(0);
}
