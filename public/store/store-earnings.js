// Stage 148.30: a salesman's own incentive statement. Loaded by store.js the
// first time it is opened. The server finds the salesman from the login.

async function renderEarnings(ctx) {
  const { api, esc, money, main, panelError } = ctx;
  main.innerHTML = '<p class="loading">Loading…</p>';
  let s;
  try {
    s = await api('/api/v1/store-app/earnings');
  } catch (e) {
    if (!e.quiet) panelError(e.message, () => renderEarnings(ctx));
    return;
  }
  if (ctx.routeName() !== 'earnings') return;
  main.innerHTML = `
    <div class="figures">
      <div class="figure wide"><span>To be paid</span><strong>${esc(money(s.unpaid))}</strong></div>
      <div class="figure"><span>Paid</span><strong>${esc(money(s.paid))}</strong></div>
    </div>
    ${s.entries.length ? `<ul class="rows">${s.entries.map(e => `<li><span>${esc(e.bill)}<small>${esc(e.date)} · ${esc(e.kind === 'Clawback' ? 'Returned' : 'Sale')}${e.paid ? ' · Paid' : ''}</small></span><span class="qty">${esc(money(e.amount))}</span></li>`).join('')}</ul>`
      : '<p class="empty">Nothing yet.</p>'}`;
}

export const routes = {
  earnings: { title: 'My earnings', render: renderEarnings },
};
