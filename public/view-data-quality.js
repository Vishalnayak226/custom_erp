// Stage 170.41: the data-quality queue - duplicates, missing HSN, bad GSTIN /
// PAN. Scan adds new problems and closes fixed ones; Ignore needs a reason.

export async function renderDataQualityView(container) {
  const res = await apiFetch('/api/v1/data-quality');
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Data quality could not be loaded.'); return; }
  const rows = (await res.json()) || [];
  const { txnPagedSection, txnRestorePane } = await import('./txn-screen.js' + new URL(import.meta.url).search);
  const esc = escapeHTMLText;
  const open = rows.filter(r => r.status === 'Open').length;

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.style.padding = '24px';
  form.innerHTML = '<button type="button" class="btn btn-primary" id="dq-scan">Scan now</button>';

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = rows.length === 0 ? '<p class="text-muted s-287f77">Nothing found.</p>' : `
    <div class="table-wrapper"><table><thead><tr><th>Record</th><th>Problem</th><th>Status</th><th>Found</th><th></th></tr></thead>
      <tbody>${rows.map((r, i) => `<tr>
        <td>${esc(r.record_doctype)} ${esc(r.record_id)}</td><td>${esc(r.problem)}</td>
        <td>${esc(r.status)}${r.ignore_reason ? ' · ' + esc(r.ignore_reason) : ''}</td><td>${esc(String(r.found_at || '').slice(0, 10))}</td>
        <td>${r.status === 'Open'
          ? `<button class="action-btn" data-dq-fixed="${i}">Fixed</button> <button class="action-btn" data-dq-ignore="${i}">Ignore</button>`
          : `<button class="action-btn" data-dq-reopen="${i}">Reopen</button>`}</td></tr>`).join('')}</tbody></table></div>`;

  container.appendChild(txnPagedSection({ id: 'data-quality', title: 'Data quality', page: 'Scan', count: open, newLabel: 'Scan', form, list }));
  txnRestorePane('data-quality');
  const reload = () => renderView('dataquality');
  const setStatus = async (row, status, reason) => {
    const r = await apiFetch('/api/v1/data-quality/' + encodeURIComponent(row.id) + '/status', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status, reason }) });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Could not change it.'); return; }
    reload();
  };
  form.querySelector('#dq-scan').addEventListener('click', async () => {
    const r = await apiFetch('/api/v1/data-quality/scan', { method: 'POST' });
    if (!r) return;
    if (!r.ok) { await showApiError(r, 'Scan failed.'); return; }
    const s = await r.json();
    showToast(`Scan done: ${s.opened || 0} new, ${s.fixed || 0} closed.`);
    reload();
  });
  list.addEventListener('click', async (e) => {
    const b = e.target.closest('button'); if (!b) return;
    const pick = (k) => rows[Number(b.dataset[k])];
    if (b.dataset.dqFixed !== undefined) await setStatus(pick('dqFixed'), 'Fixed', '');
    else if (b.dataset.dqReopen !== undefined) await setStatus(pick('dqReopen'), 'Open', '');
    else if (b.dataset.dqIgnore !== undefined) {
      const reason = await showCustomPrompt('Reason', '', 'Ignore');
      if (reason == null) return;
      await setStatus(pick('dqIgnore'), 'Ignored', reason.trim());
    }
  });
}
