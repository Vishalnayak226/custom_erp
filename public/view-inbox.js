// Stage 73.3.3: one task inbox. Native ES module, loaded on first visit to
// the Approvals menu. A read-only aggregation (GET /api/v1/inbox) of the
// approvals routed to this role, the order desk's exception queues and the
// catalog tasks assigned here; every row opens the screen whose engine acts
// on it. The approvals table itself is the existing one, embedded below, so
// approve/reject behave exactly as before.
// The approvals table comes from the procurement module through the shell's
// own loader (same URL, exports published for its Approve/Reject buttons) -
// a static import here would load a second, unpublished copy.
const loadApprovals = () => loadViewModule('/view-procurement.js' + new URL(import.meta.url).search);

const INBOX_SOURCES = [
  { id: '', label: 'Everything' },
  { id: 'Approval', label: 'Approvals' },
  { id: 'Order desk', label: 'Order desk' },
  { id: 'Catalog', label: 'Catalog' }
];
let inboxSourceFilter = '';

function inboxWhen(value) {
  if (!value || String(value).startsWith('0001-')) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString();
}

async function renderTaskInboxView(container) {
  container.innerHTML = `
    <div class="page-header"><div class="page-title-section"><h1 class="page-title">My work</h1></div>
      <span class="page-section-actions"><button class="btn btn-outline" id="inbox-refresh">Refresh</button></span></div>
    <div class="table-panel" style="padding:16px 24px;margin-bottom:24px;">
      <div id="inbox-filters" role="group" aria-label="Show" style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px;"></div>
      <div id="inbox-errors"></div>
      <div id="inbox-list" class="table-wrapper"><p class="text-muted">Loading your work…</p></div>
    </div>
    <div id="inbox-approvals"></div>`;
  document.getElementById('inbox-refresh').addEventListener('click', () => renderView('approvals'));

  const approvalsHost = document.getElementById('inbox-approvals');
  const [res] = await Promise.all([apiFetch('/api/v1/inbox'), loadApprovals().then(m => m.renderApprovalsView(approvalsHost))]);
  // The embedded screen keeps its own table and actions; only its heading
  // steps down a level so the page has one title.
  const embeddedTitle = approvalsHost.querySelector('h1.page-title');
  if (embeddedTitle) {
    const h2 = document.createElement('h2');
    h2.textContent = 'Approvals waiting for a decision';
    h2.style.cssText = 'font-size:16px;margin:0;';
    embeddedTitle.replaceWith(h2);
  }

  const list = document.getElementById('inbox-list');
  if (!res) { renderErrorPanel(list, 'Could not reach the server to load your work.', () => renderView('approvals')); return; }
  if (!res.ok) { list.innerHTML = ''; await showApiError(res, 'Failed to load your work.'); return; }
  const inbox = await res.json();
  const items = inbox.items || [];
  const counts = inbox.counts || {};

  const filters = document.getElementById('inbox-filters');
  const countFor = id => id === '' ? items.length : items.filter(i => i.source === id).length;
  filters.innerHTML = INBOX_SOURCES.map(s => `
    <button class="btn btn-sm ${s.id === inboxSourceFilter ? 'btn-primary' : 'btn-outline'}" data-inbox-source="${escapeHTMLText(s.id)}" aria-pressed="${s.id === inboxSourceFilter}">
      ${escapeHTMLText(s.label)} <span class="badge">${countFor(s.id)}</span></button>`).join('');
  filters.querySelectorAll('[data-inbox-source]').forEach(btn => btn.addEventListener('click', () => {
    inboxSourceFilter = btn.dataset.inboxSource;
    renderView('approvals');
  }));

  const errors = Object.entries(inbox.errors || {});
  document.getElementById('inbox-errors').innerHTML = errors.length
    ? `<p class="text-muted" role="status">Some work could not be loaded: ${errors.map(([k, v]) => `${escapeHTMLText(k)} (${escapeHTMLText(v)})`).join('; ')}. The rest is shown.</p>`
    : '';

  const shown = items.filter(i => !inboxSourceFilter || i.source === inboxSourceFilter);
  if (shown.length === 0) {
    list.innerHTML = `<p class="text-muted oms-detail-empty">${items.length === 0
      ? 'Nothing is waiting for you. Approvals routed to your role, stuck orders and catalog tasks assigned to you will appear here.'
      : 'Nothing of this kind is waiting for you.'}</p>`;
    return;
  }
  list.innerHTML = `<table>
    <thead><tr><th>Where</th><th>What</th><th>Why it is yours</th><th>Waiting since</th><th>Due</th><th></th></tr></thead>
    <tbody>${shown.map((item, idx) => `<tr>
      <td>${escapeHTMLText(item.source)}</td>
      <td>${escapeHTMLText(item.title)}${item.overdue ? ' <span class="badge badge-danger">Overdue</span>' : ''}</td>
      <td>${escapeHTMLText(item.reason || '')}</td>
      <td>${escapeHTMLText(inboxWhen(item.waiting_since))}</td>
      <td>${escapeHTMLText(item.due_date || '—')}</td>
      <td><button class="action-btn" data-inbox-open="${idx}">Open</button></td>
    </tr>`).join('')}</tbody></table>`;
  list.querySelectorAll('[data-inbox-open]').forEach(btn => btn.addEventListener('click', () => openInboxItem(shown[Number(btn.dataset.inboxOpen)])));
  if (counts.approvals === 0 && approvalsHost.querySelector('table') === null) approvalsHost.hidden = true;
}

function openInboxItem(item) {
  if (item.view === 'approvals') {
    // Approvals are decided in the table below - bring the row into view.
    const row = Array.from(document.querySelectorAll('#inbox-approvals tr')).find(tr => tr.textContent.includes(item.doc_id));
    (row || document.getElementById('inbox-approvals')).scrollIntoView({ block: 'center' });
    row?.querySelector('button')?.focus();
    return;
  }
  if (item.view === 'oms') {
    try { sessionStorage.setItem('erp_oms_open_queue', item.queue || ''); } catch (_) { /* storage off: the queue buttons are on the page */ }
    setActiveMenu('menu-oms');
    renderView('oms');
    return;
  }
  if (item.view === 'import-job') {
    // Stage 55.11: an import with failed rows opens straight on its resume.
    loadViewModule('/view-import-jobs.js' + new URL(import.meta.url).search).then(m => m.openImportResume(item.doc_id));
    return;
  }
  if (item.view === 'pim' && item.doctype === 'PIMImportSchedule') {
    currentPIMTab = 'import-schedules';
    setActiveMenu('menu-pim');
    renderView('pim');
    return;
  }
  if (item.view === 'pim') {
    currentPIMTab = 'my-work';
    setActiveMenu('menu-pim');
    renderView('pim');
  }
}

export { renderTaskInboxView };
