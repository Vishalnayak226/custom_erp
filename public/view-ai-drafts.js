// Stage 148.37: drafts left by an AI assistant, in the Orders list. A person
// checks one and presses Submit; the order is then made under their name.
// Shown only when the ai_access module is on and the user may make orders.

const aiWhen = (v) => { const d = new Date(v); return Number.isNaN(d.getTime()) ? '' : formatDateTime(d); };

export async function renderAssistantDrafts(host) {
  if (!host) return;
  const res = await apiFetch('/api/v1/orders/assistant-drafts');
  // Off, or not allowed: nothing to show.
  if (!res || !res.ok) { host.remove(); return; }
  const { drafts = [] } = await res.json();
  if (drafts.length === 0) { host.remove(); return; }
  const money = (n) => (typeof formatMoney === 'function' ? formatMoney(n) : Number(n).toFixed(2));
  host.innerHTML = `
    <h2 class="s-78a190">Assistant drafts</h2>
    <div class="table-wrapper"><table>
      <thead><tr><th>Draft</th><th>Customer</th><th>Store</th><th>Lines</th><th>Total</th><th>Made</th><th>Actions</th></tr></thead>
      <tbody>${drafts.map(d => `<tr>
        <td>${escapeHTMLText(d.id)}</td><td>${escapeHTMLText(d.customer_name || '')}</td><td>${escapeHTMLText(d.location_code || '')}</td>
        <td class="num">${escapeHTMLText(d.line_count)}</td><td class="num">${escapeHTMLText(money(d.total_amount))}</td>
        <td>${escapeHTMLText(aiWhen(d.created_at))}</td>
        <td><button class="action-btn" data-ai-submit="${escapeHTMLText(d.id)}">Submit</button></td>
      </tr>`).join('')}</tbody>
    </table></div>`;
  host.querySelectorAll('[data-ai-submit]').forEach(btn => btn.addEventListener('click', async () => {
    if (!await showCustomConfirm('Submit this draft as an order?')) return;
    await guardAgainstDoubleSubmit(btn, 'Submitting…', async () => {
      const r = await apiFetch(`/api/v1/orders/${encodeURIComponent(btn.dataset.aiSubmit)}/submit-assistant-draft`, { method: 'POST', body: '{}' });
      if (!r) return;
      if (!r.ok) { await showApiError(r, 'Not submitted.'); return; }
      showToast('Order made.', { variant: 'success' });
      renderView('oms');
    });
  }));
}
