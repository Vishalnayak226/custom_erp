// 297.3: a progress bar for every long task.
//
// The Tasks button in the top bar lists the user's own long work (imports,
// report exports, image copies) with a bar, rows done and time left, while
// the user carries on with other screens. app.js loads this module only once
// a task exists: trackTask() when a screen starts one, or at sign-in when
// the browser still holds a running task's id (a reload, another tab).
//
// The server is asked one small question (GET /api/v1/me/tasks) every few
// seconds, and only while a task of this user is still running - a tab with
// nothing running sends nothing. The running ids live in localStorage, so a
// reload or a second tab picks the bar up again.

const POLL_MS = 3000;
const POLL_HIDDEN_MS = 15000;

let tasks = [];            // the last answer, running first
const local = new Map();   // 297.2: work this tab does itself (a file going up), shown first
const shown = () => [...local.values(), ...tasks];
let timer = null;
let inflight = false;
const watchers = new Set(); // fn(tasks) - a screen showing its own task's bar
const announced = new Set(); // ids whose finish this tab has shown

function storeKey() {
  return `erp_tasks:${localStorage.getItem('erp_tenant_id') || 'default'}:${localStorage.getItem('erp_username') || ''}`;
}
function storedIds() {
  try { return JSON.parse(localStorage.getItem(storeKey()) || '[]'); } catch (_) { return []; }
}
function saveIds(ids) {
  try {
    if (ids.length) localStorage.setItem(storeKey(), JSON.stringify(ids));
    else localStorage.removeItem(storeKey());
  } catch (_) { /* private window: the bar still runs for this tab */ }
}

const active = t => t.state === 'queued' || t.state === 'running';

function schedule(ms) {
  clearTimeout(timer);
  timer = setTimeout(poll, ms);
}

async function poll() {
  timer = null;
  if (inflight) return;
  inflight = true;
  try {
    const res = await window.apiFetch('/api/v1/me/tasks').catch(() => null);
    if (res && res.ok) apply((await res.json()).tasks || []);
  } finally {
    inflight = false;
  }
  if (storedIds().length) schedule(document.hidden ? POLL_HIDDEN_MS : POLL_MS);
}

function apply(list) {
  const byId = new Map(list.map(t => [t.id, t]));
  const still = [];
  for (const id of storedIds()) {
    const t = byId.get(id);
    if (!t) continue; // gone from the list (removed, or older than a day)
    if (active(t)) still.push(id);
    else announce(t);
  }
  saveIds(still);
  tasks = list;
  render();
  for (const fn of watchers) { try { fn(shown()); } catch (_) { /* a closed screen */ } }
}

function announce(t) {
  if (announced.has(t.id)) return;
  announced.add(t.id);
  const failed = t.state === 'failed';
  if (t.state === 'cancelled') return;
  window.showToast?.(t.label || 'Task', {
    title: failed ? 'Failed' : 'Done',
    variant: failed ? 'danger' : 'success',
    ms: 10000,
    onClick: () => openPanel(),
  });
}

// tasksStart is called by trackTask() in app.js the moment a screen has
// queued a task: the button shows at once and polling begins.
export function tasksStart(id, label) {
  const ids = storedIds();
  if (!ids.includes(id)) { ids.push(id); saveIds(ids); }
  if (!tasks.some(t => t.id === id)) {
    tasks = [{ id, label: label || 'Task', state: 'queued', pct: -1, created_at: new Date().toISOString() }, ...tasks];
  }
  render();
  for (const fn of watchers) { try { fn(shown()); } catch (_) { /* */ } }
  schedule(POLL_MS);
}

// tasksLocal shows work this tab does itself - a large file going up in
// pieces (297.2) - with the server's tasks: {id, label, state, pct, done,
// total, eta_sec, note, onCancel}. State "gone" takes it off; a finished one
// is announced like a server task and taken off.
export function tasksLocal(t) {
  if (!t || !t.id) return;
  if (t.state === 'gone') local.delete(t.id);
  else if (active(t)) local.set(t.id, { ...t, local: true });
  else { local.delete(t.id); announce(t); }
  render();
  for (const fn of watchers) { try { fn(shown()); } catch (_) { /* */ } }
}

// tasksResume picks running tasks up again after a reload or from another tab.
export function tasksResume() {
  if (!timer && !inflight) poll();
}

// tasksWatch lets a screen show the live bar of its own task again when the
// user comes back to it. fn gets every task; the screen picks its own.
export function tasksWatch(fn) {
  watchers.add(fn);
  fn(shown());
  return () => watchers.delete(fn);
}

// ---- the button and its panel ----

function ensureButton() {
  let btn = document.getElementById('tasks-btn');
  if (btn) return btn;
  const bar = document.querySelector('.header-actions');
  if (!bar) return null;
  btn = document.createElement('button');
  btn.type = 'button';
  btn.id = 'tasks-btn';
  btn.className = 'btn btn-outline btn-icon tasks-btn';
  btn.title = 'Tasks';
  btn.setAttribute('aria-label', 'Tasks');
  btn.setAttribute('aria-expanded', 'false');
  btn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="4" y1="6" x2="20" y2="6"></line><line x1="4" y1="12" x2="14" y2="12"></line><line x1="4" y1="18" x2="10" y2="18"></line></svg><span class="tasks-count hidden"></span>';
  btn.addEventListener('click', () => (panelOpen() ? closePanel() : openPanel()));
  bar.insertBefore(btn, document.getElementById('sync-btn') || bar.firstChild);
  return btn;
}

const panelOpen = () => !!document.getElementById('tasks-panel') && !document.getElementById('tasks-panel').classList.contains('hidden');

function openPanel() {
  const btn = ensureButton();
  if (!btn) return;
  let panel = document.getElementById('tasks-panel');
  if (!panel) {
    panel = document.createElement('div');
    panel.id = 'tasks-panel';
    panel.className = 'tasks-panel';
    panel.setAttribute('role', 'dialog');
    panel.setAttribute('aria-label', 'Tasks');
    panel.addEventListener('click', onPanelClick);
    document.body.appendChild(panel);
    document.addEventListener('mousedown', e => {
      if (panelOpen() && !panel.contains(e.target) && !btn.contains(e.target)) closePanel();
    });
    document.addEventListener('keydown', e => { if (e.key === 'Escape' && panelOpen()) closePanel(); });
  }
  panel.classList.remove('hidden');
  btn.setAttribute('aria-expanded', 'true');
  render();
  if (!timer && !inflight) poll(); // a fresh read when opened, running or not
}

function closePanel() {
  document.getElementById('tasks-panel')?.classList.add('hidden');
  document.getElementById('tasks-btn')?.setAttribute('aria-expanded', 'false');
}

const fmt = n => Number(n || 0).toLocaleString('en-IN');

function timeLeft(sec) {
  if (!sec) return '';
  if (sec < 60) return `${sec} s left`;
  if (sec < 3600) return `${Math.round(sec / 60)} min left`;
  return `${Math.floor(sec / 3600)} h ${Math.round((sec % 3600) / 60)} min left`;
}

function stateText(t) {
  switch (t.state) {
    case 'queued': return 'Waiting';
    case 'running': return timeLeft(t.eta_sec) || (t.pct >= 0 ? `${t.pct}%` : 'Running');
    case 'done': return 'Done';
    case 'failed': return 'Failed';
    default: return 'Cancelled';
  }
}

function row(t) {
  const el = document.createElement('div');
  el.className = `task-row task-${t.state}`;
  el.dataset.id = t.id;
  const head = document.createElement('div');
  head.className = 'task-head';
  const label = document.createElement('span');
  label.className = 'task-label';
  label.textContent = t.label || 'Task';
  const state = document.createElement('span');
  state.className = 'task-state';
  state.textContent = stateText(t);
  head.append(label, state);
  el.appendChild(head);

  if (active(t)) {
    // <progress> needs no inline style; with no value it shows as busy.
    const bar = document.createElement('progress');
    bar.max = 100;
    if (t.pct >= 0 && t.state === 'running') bar.value = t.pct;
    bar.setAttribute('aria-label', t.label || 'Task');
    el.appendChild(bar);
  }

  const foot = document.createElement('div');
  foot.className = 'task-foot';
  const r = t.result || {};
  if (t.note) foot.append(t.note); // a local task says what it counts itself (MB sent)
  else if (t.total) foot.append(`${fmt(t.done)} of ${fmt(t.total)}`); // rows, photos - whatever the task counts
  else if (t.state === 'done' && (r.rows_in != null || r.rows_bad != null)) {
    foot.append(`${fmt(r.rows_in)} in${r.rows_bad ? ` · ${fmt(r.rows_bad)} not` : ''}`);
  }
  if (t.state === 'failed' && t.error) foot.append(t.error);
  const act = (name, text) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'btn btn-ghost btn-sm';
    b.dataset.act = name;
    b.textContent = text;
    foot.append(' ', b);
  };
  if (active(t)) act('cancel', 'Cancel');
  if (t.state === 'done' && t.kind === 'export') act('download', 'Download');
  if (r.error_file) act('errors', 'Error file');
  if (foot.childNodes.length) el.appendChild(foot);
  return el;
}

function render() {
  const btn = ensureButton();
  if (!btn) return;
  const all = shown();
  const running = all.filter(active).length;
  btn.classList.toggle('hidden', all.length === 0);
  btn.classList.toggle('tasks-busy', running > 0);
  const count = btn.querySelector('.tasks-count');
  count.textContent = running ? String(running) : '';
  count.classList.toggle('hidden', !running);
  btn.title = running ? `Tasks · ${running} running` : 'Tasks';

  const panel = document.getElementById('tasks-panel');
  if (!panel || panel.classList.contains('hidden')) return;
  const title = document.createElement('div');
  title.className = 'tasks-title';
  title.textContent = 'Tasks';
  const list = document.createElement('div');
  list.className = 'tasks-list';
  if (!all.length) {
    const empty = document.createElement('div');
    empty.className = 'task-empty';
    empty.textContent = 'No tasks today';
    list.appendChild(empty);
  }
  for (const t of all) list.appendChild(row(t));
  panel.replaceChildren(title, list);
}

async function onPanelClick(e) {
  const b = e.target.closest('button[data-act]');
  if (!b) return;
  const t = shown().find(x => x.id === b.closest('.task-row')?.dataset.id);
  if (!t) return;
  if (t.local) { if (b.dataset.act === 'cancel') t.onCancel?.(); return; }
  b.disabled = true;
  try {
    if (b.dataset.act === 'cancel') {
      const res = await window.apiFetch(`/api/v1/me/tasks/${encodeURIComponent(t.id)}/cancel`, { method: 'POST', body: '{}' });
      if (res && !res.ok) await window.showApiError?.(res, 'The task could not be cancelled.');
      poll();
    } else if (b.dataset.act === 'download') {
      await download(`/api/v1/reports/export/${encodeURIComponent(t.id.slice('export:'.length))}?download=1`, t.label, 'csv');
    } else if (b.dataset.act === 'errors') {
      await download(t.result.error_file, `${t.label} errors`, 'csv');
    }
  } finally {
    b.disabled = false;
  }
}

// A download goes through apiFetch (the token is a header, never in a link)
// and reaches the browser as a Blob.
async function download(url, name, ext) {
  const res = await window.apiFetch(url);
  if (!res) return;
  if (!res.ok) { await window.showApiError?.(res, 'The file could not be downloaded.'); return; }
  const blobUrl = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = blobUrl;
  const base = String(name || 'task').replace(/^Export · /, '').replace(/[^A-Za-z0-9]+/g, '-').replace(/^-|-$/g, '');
  a.download = `${base}-${new Date().toISOString().slice(0, 10)}.${ext}`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(blobUrl);
}

document.addEventListener('visibilitychange', () => {
  // Back on the tab: read at once rather than wait out the slow hidden pace.
  if (!document.hidden && storedIds().length && !inflight) schedule(0);
});
window.addEventListener('storage', e => {
  if (e.key === storeKey() && e.newValue && !timer && !inflight) poll();
});
