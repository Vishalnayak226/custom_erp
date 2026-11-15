// 50.12 (ask 193): Subscription - a client Admin's own plan, this month's use
// against the plan's quota, and the platform's invoices. Native ES module.
// Reads GET /api/v1/saas/subscription only; nothing here changes anything.

function subRupees(paise) {
  return '₹' + (Number(paise || 0) / 100).toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function subDay(v) {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? String(v) : formatDateTime(d, true);
}

async function renderSubscriptionView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = '<div class="page-title-section"><h1 class="page-title">Subscription</h1></div>';
  container.appendChild(header);

  const res = await apiFetch('/api/v1/saas/subscription');
  if (!res) return;
  if (!res.ok) {
    const msg = document.createElement('p');
    msg.className = 'text-muted';
    msg.textContent = res.status === 403 ? 'Only an Admin can see the subscription.' : 'The subscription could not be loaded.';
    container.appendChild(msg);
    return;
  }
  const { subscription: sub, usage, invoices = [] } = await res.json();

  if (!sub) {
    const empty = document.createElement('div');
    empty.className = 'table-panel';
    empty.innerHTML = '<p class="text-muted" id="subscription-empty">No subscription.</p>';
    container.appendChild(empty);
    return;
  }

  const summary = document.createElement('div');
  summary.className = 'table-panel';
  summary.id = 'subscription-summary';
  summary.innerHTML = `<table><tbody>
    <tr><th>Status</th><td>${escapeHTMLText(sub.explanation || sub.state || '')}</td></tr>
    <tr><th>Plan</th><td>${escapeHTMLText(sub.plan_key || '')}</td></tr>
    <tr><th>Period ends</th><td>${escapeHTMLText(subDay(sub.period_end))}</td></tr>
    ${sub.grace_until ? `<tr><th>Grace until</th><td>${escapeHTMLText(subDay(sub.grace_until))}</td></tr>` : ''}
  </tbody></table>`;
  container.appendChild(summary);

  const use = document.createElement('div');
  use.className = 'table-panel';
  use.id = 'subscription-usage';
  const meters = (usage && usage.meters) || [];
  use.innerHTML = `<table>
    <thead><tr><th>This month</th><th>Used</th><th>Included</th><th>Over</th></tr></thead>
    <tbody>${meters.map(m => `<tr>
      <td>${escapeHTMLText(m.label || m.meter)}</td>
      <td>${Number(m.used || 0).toLocaleString('en-IN')}</td>
      <td>${m.quota > 0 ? Number(m.quota).toLocaleString('en-IN') : '—'}</td>
      <td>${m.overage_paise > 0 ? escapeHTMLText(subRupees(m.overage_paise)) : '—'}</td></tr>`).join('')}</tbody></table>`;
  container.appendChild(use);

  const inv = document.createElement('div');
  inv.className = 'table-panel';
  inv.id = 'subscription-invoices';
  if (!invoices.length) {
    inv.innerHTML = '<p class="text-muted">No invoices yet.</p>';
  } else {
    inv.innerHTML = `<table>
      <thead><tr><th>Invoice</th><th>Date</th><th>Description</th><th>Taxable</th><th>CGST</th><th>SGST</th><th>IGST</th><th>Total</th></tr></thead>
      <tbody>${invoices.map(v => `<tr>
        <td>${v.status === 'pending' ? 'Pending' : escapeHTMLText(v.invoice_no)}</td>
        <td>${escapeHTMLText(subDay(v.date))}</td>
        <td>${escapeHTMLText(v.description || '')}</td>
        <td>${escapeHTMLText(subRupees(v.taxable_paise))}</td>
        <td>${escapeHTMLText(subRupees(v.cgst_paise))}</td>
        <td>${escapeHTMLText(subRupees(v.sgst_paise))}</td>
        <td>${escapeHTMLText(subRupees(v.igst_paise))}</td>
        <td>${escapeHTMLText(subRupees(v.gross_paise))}</td></tr>`).join('')}</tbody></table>`;
  }
  container.appendChild(inv);
}

export { renderSubscriptionView };
