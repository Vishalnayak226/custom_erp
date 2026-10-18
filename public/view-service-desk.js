// Stage 55.18: the service desk and project context. Native ES module,
// loaded by the generic record list (view-documents.js) for ServiceTicket,
// ServiceContract and Project, and rendered above their table. Read-only
// views (GET /api/v1/service/desk, /service/contracts/{id},
// /projects/{id}/ledger); ticket steps run through the list's own
// runServiceTicketStep, so the lifecycle has one implementation.

function deskWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString();
}

const DESK_STEP = { Draft: 'assign', Assigned: 'start', InProgress: 'resolve', Resolved: 'close' };
let deskMineOnly = true;

async function renderServiceDeskPanel(container, doctype) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.cssText = 'padding:16px 24px;margin-bottom:24px;';
  container.appendChild(panel);
  if (doctype === 'ServiceTicket') return renderTicketDesk(panel);
  if (doctype === 'ServiceContract') return renderLookup(panel, 'Contract coverage', 'Contract number', '/api/v1/service/contracts/', renderContract);
  if (doctype === 'Project') return renderLookup(panel, 'Project cost and revenue', 'Project code', '/api/v1/projects/', renderProjectLedger, '/ledger');
}

async function renderTicketDesk(panel) {
  panel.innerHTML = '<h2 style="font-size:16px;margin:0 0 8px;">Service desk</h2><p class="text-muted">Loading…</p>';
  const res = await apiFetch(`/api/v1/service/desk${deskMineOnly ? '?mine=1' : ''}`);
  if (!res) return;
  if (!res.ok) { panel.innerHTML = '<h2 style="font-size:16px;margin:0 0 8px;">Service desk</h2><p class="text-muted">The service desk could not be loaded.</p>'; return; }
  const { tickets = [] } = await res.json();
  panel.innerHTML = `
    <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:8px;">
      <h2 style="font-size:16px;margin:0;flex:1;">Service desk</h2>
      <button class="btn btn-sm ${deskMineOnly ? 'btn-primary' : 'btn-outline'}" data-desk-scope="mine" aria-pressed="${deskMineOnly}">Mine and unassigned</button>
      <button class="btn btn-sm ${deskMineOnly ? 'btn-outline' : 'btn-primary'}" data-desk-scope="all" aria-pressed="${!deskMineOnly}">All open</button>
    </div>
    ${tickets.length === 0 ? `<p class="text-muted oms-detail-empty">No open tickets ${deskMineOnly ? 'for you or unassigned' : ''}. New tickets appear here as they are logged.</p>` : `
    <div class="table-wrapper"><table>
      <thead><tr><th>Ticket</th><th>Customer</th><th>Priority</th><th>Status</th><th>Assigned</th><th>Due</th><th>Contract</th><th>Next step</th></tr></thead>
      <tbody>${tickets.map(t => `<tr>
        <td><button class="action-btn" data-desk-history="${escapeHTMLText(t.id)}" title="Show status history">${escapeHTMLText(t.id)}</button><div class="text-muted" style="font-size:12px;">${escapeHTMLText(t.description || '')}</div></td>
        <td>${escapeHTMLText(t.customer || '')}</td>
        <td>${escapeHTMLText(t.priority || '')}</td>
        <td>${escapeHTMLText(t.status)}</td>
        <td>${escapeHTMLText(t.assigned_to || 'Unassigned')}</td>
        <td>${t.due ? `<span class="badge ${t.overdue ? 'badge-danger' : 'badge-warning'}">${escapeHTMLText(t.due)}</span>` : escapeHTMLText(t.resolve_by_date || '—')}</td>
        <td>${t.contract ? `${escapeHTMLText(t.contract.id)}<div class="text-muted" style="font-size:12px;">${escapeHTMLText(t.contract.coverage)}${t.contract.covered ? ` - ${escapeHTMLText(String(t.contract.visits_remaining))} visit(s) left` : ''}</div>` : '<span class="text-muted">No contract</span>'}</td>
        <td>${DESK_STEP[t.status] ? `<button class="btn btn-outline btn-sm" data-desk-step="${DESK_STEP[t.status]}" data-desk-ticket="${escapeHTMLText(t.id)}">${escapeHTMLText(t.next_step)}</button>` : ''}</td>
      </tr>
      <tr class="hidden" data-desk-history-row="${escapeHTMLText(t.id)}"><td colspan="8">${(t.history || []).length === 0
        ? '<span class="text-muted">No status changes yet - logged ' + escapeHTMLText(deskWhen(t.created_at)) + '.</span>'
        : `<ol style="margin:0;padding-left:18px;">${t.history.map(h => `<li>${escapeHTMLText(deskWhen(h.at))} - ${escapeHTMLText(h.change)}${h.by ? ` (${escapeHTMLText(h.by)})` : ''}</li>`).join('')}</ol>`}</td></tr>`).join('')}</tbody>
    </table></div>`}`;
  panel.querySelectorAll('[data-desk-scope]').forEach(b => b.addEventListener('click', () => { deskMineOnly = b.dataset.deskScope === 'mine'; renderTicketDesk(panel); }));
  panel.querySelectorAll('[data-desk-history]').forEach(b => b.addEventListener('click', () => {
    panel.querySelector(`[data-desk-history-row="${CSS.escape(b.dataset.deskHistory)}"]`)?.classList.toggle('hidden');
  }));
  panel.querySelectorAll('[data-desk-step]').forEach(b => b.addEventListener('click', () => window.runServiceTicketStep(b.dataset.deskTicket, b.dataset.deskStep, b)));
}

function renderLookup(panel, title, label, base, render, suffix = '') {
  panel.innerHTML = `
    <h2 style="font-size:16px;margin:0 0 8px;">${escapeHTMLText(title)}</h2>
    <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end;">
      <div class="form-group" style="margin-bottom:0;"><label class="form-label" for="desk-lookup">${escapeHTMLText(label)}</label>
        <input id="desk-lookup" class="form-input" style="width:240px;" placeholder="Pick from the list below or type it"></div>
      <button class="btn btn-outline" id="desk-lookup-go">Show</button>
    </div>
    <div id="desk-lookup-result" style="margin-top:12px;"></div>`;
  const go = async () => {
    const id = panel.querySelector('#desk-lookup').value.trim();
    const host = panel.querySelector('#desk-lookup-result');
    if (!id) { host.innerHTML = `<p class="text-muted">Enter a ${escapeHTMLText(label.toLowerCase())}, or use a row's code from the list below.</p>`; return; }
    host.innerHTML = '<p class="text-muted">Loading…</p>';
    const res = await apiFetch(base + encodeURIComponent(id) + suffix);
    if (!res) return;
    if (!res.ok) { host.innerHTML = ''; await showApiError(res, `Could not load ${id}.`); return; }
    host.innerHTML = render(await res.json());
  };
  panel.querySelector('#desk-lookup-go').addEventListener('click', go);
  panel.querySelector('#desk-lookup').addEventListener('keydown', e => { if (e.key === 'Enter') go(); });
  if (typeof attachLinkTypeahead === 'function') attachLinkTypeahead(panel.querySelector('#desk-lookup'), title.startsWith('Project') ? 'Project' : 'ServiceContract');
}

function renderContract(c) {
  return `<p><strong>${escapeHTMLText(c.id)}</strong> - ${escapeHTMLText(c.customer || '')}: <span class="badge ${c.covered ? 'badge-success' : 'badge-warning'}">${escapeHTMLText(c.coverage)}</span>
    ${escapeHTMLText(String(c.visits_used))} of ${escapeHTMLText(String(c.visits_included))} visits used, ${escapeHTMLText(c.start_date || '?')} to ${escapeHTMLText(c.end_date || '?')}.</p>
    ${(c.tickets || []).length === 0 ? '<p class="text-muted">No tickets under this contract yet.</p>' : `<div class="table-wrapper"><table>
      <thead><tr><th>Ticket</th><th>Status</th><th>Assigned</th><th>Logged</th><th>Description</th></tr></thead>
      <tbody>${c.tickets.map(t => `<tr><td>${escapeHTMLText(t.id)}</td><td>${escapeHTMLText(t.status)}</td><td>${escapeHTMLText(t.assigned_to || 'Unassigned')}</td><td>${escapeHTMLText(deskWhen(t.created_at))}</td><td>${escapeHTMLText(t.description)}</td></tr>`).join('')}</tbody></table></div>`}`;
}

function renderProjectLedger(p) {
  return `<p><strong>${escapeHTMLText(p.project)}</strong> ${escapeHTMLText(p.name || '')}${p.customer ? ` - ${escapeHTMLText(p.customer)}` : ''} <span class="text-muted">(${escapeHTMLText(p.status || '')})</span></p>
    <p>Revenue ${formatMoney(p.revenue)} - expense ${formatMoney(p.expense)} = <strong>net ${formatMoney(p.net)}</strong></p>
    ${(p.documents || []).length === 0 ? '<p class="text-muted">Nothing has posted to this project yet. Journal vouchers and expense claims carry a Project field.</p>' : `<div class="table-wrapper"><table>
      <thead><tr><th>Posted</th><th>Document</th><th class="num">Revenue</th><th class="num">Expense</th></tr></thead>
      <tbody>${p.documents.map(d => `<tr><td>${escapeHTMLText(deskWhen(d.posted_at))}</td><td>${escapeHTMLText(d.document_type)} ${escapeHTMLText(d.document_id)}</td><td class="num">${formatMoney(d.revenue)}</td><td class="num">${formatMoney(d.expense)}</td></tr>`).join('')}</tbody></table></div>`}`;
}

export { renderServiceDeskPanel };
