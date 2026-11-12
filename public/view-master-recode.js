// Stage 93.11 (user, 2026-10-18, on Department/HQ/2026/000001 and
// LegalEntity/HQ/2026/000001): "Clean them and ask to set them once. Then it
// should not change code ... it should suggest and Mandatory to user."
//
// A master list that still holds records on an old long code shows this one
// panel: the code format - asked only when it was never set, with the
// suggestion filled in - the old -> new list, and Renumber. Every record that
// points at an old code is updated with it (engines/master_recode.go). Once
// nothing is left on a long code the panel does not appear again. Admin only,
// like the endpoint behind it.
//
// Stage 76.6 (D1, "Re-code everything"): every master, not just the first
// four; "Show what changes" lists each table and column it rewrites with row
// counts; Renumber waits for a backup from the last 24 hours; Item codes carry
// a warning (stickers, channel SKUs) the admin ticks before renumbering.

const esc = (s) => window.escapeHTMLText(String(s ?? ''));

async function loadPlan(doctype, prefix, length, opts = {}) {
  const qs = new URLSearchParams();
  if (prefix) { qs.set('prefix', prefix); qs.set('length', length); }
  if (opts.detail) qs.set('detail', '1');
  const res = await apiFetch(`/api/v1/master-codes/${encodeURIComponent(doctype)}/clean${qs.toString() ? `?${qs}` : ''}`);
  if (!res) return null;
  if (!res.ok) {
    if (!opts.quiet) await showApiError(res, 'Could not work out the new codes.');
    return null;
  }
  return res.json();
}

export async function renderMasterRecodePanel(host, doctype, label) {
  // Opened on every master list for an admin: a list with nothing to clean
  // (or a type the server does not clean) just shows nothing.
  let plan = await loadPlan(doctype, '', 0, { quiet: true });
  if (!plan || !Array.isArray(plan.rows) || plan.rows.length === 0) { host.remove(); return; }

  const panel = document.createElement('div');
  panel.className = 'table-panel master-recode';
  panel.style.cssText = 'padding: 16px 20px; margin-bottom: 16px;';
  host.appendChild(panel);

  const paint = () => {
    const n = plan.rows.length;
    const backup = plan.backup || { ok: true };
    const lengths = [3, 4, 5, 6, 7, 8].map(d => `<option value="${d}" ${d === plan.length ? 'selected' : ''}>${d}</option>`).join('');
    panel.innerHTML = `
      <div class="s-13dc58">${n} ${esc(label)} record${n === 1 ? ' is' : 's are'} still on an old long code</div>
      ${plan.configured
        ? `<div class="txn-hint s-16949d">${esc(label)} codes are <b>${esc(plan.prefix)}</b> and ${plan.length} digits.</div>`
        : `<div class="s-1262ae">
            <span class="form-label s-2a0ca8">${esc(label)} code format (asked once)<span class="required">*</span></span>
            <label class="s-2dab17">Prefix
              <input class="form-input s-6f5e12" name="mr-prefix" maxlength="6" value="${esc(plan.prefix)}"></label>
            <label class="s-2dab17">Digits
              <select class="form-select s-dabaad" name="mr-length">${lengths}</select></label>
            <button type="button" class="btn btn-outline" data-mr-preview>Show the new codes</button>
          </div>`}
      <table>
        <thead><tr><th>Name</th><th>Old code</th><th>New code</th></tr></thead>
        <tbody>${plan.rows.map(r => `<tr><td>${esc(r.name)}</td><td class="s-d59bb5">${esc(r.old)}</td><td class="s-21a1be">${esc(r.new)}</td></tr>`).join('')}</tbody>
      </table>
      <div class="txn-hint s-40af51">${plan.references
        ? `${plan.references} other record${plan.references === 1 ? ' points' : 's point'} at these codes.`
        : ''} All updated in one step; codes cannot change again.
        <button type="button" class="btn btn-ghost btn-sm" data-mr-detail>Show what changes</button></div>
      <div data-mr-touches></div>
      ${plan.warning ? `<div class="s-53cad6">${esc(plan.warning)}
        <label class="s-77cce6"><input type="checkbox" data-mr-ack> I will reprint the stickers and update the channel listings</label></div>` : ''}
      ${backup.required || !backup.ok ? `<div class="txn-hint" style="margin: 8px 0;${backup.ok ? '' : ' padding: 10px 12px; border-radius: 6px; background: var(--danger-soft-bg); color: var(--danger-strong);'}">${esc(backup.note || '')}</div>` : ''}
      <button type="button" class="btn btn-primary" data-mr-apply ${backup.ok ? '' : 'disabled'}>Renumber ${n} record${n === 1 ? '' : 's'}</button>`;

    const prefix = panel.querySelector('[name="mr-prefix"]');
    const length = panel.querySelector('[name="mr-length"]');
    prefix?.addEventListener('input', () => {
      prefix.value = prefix.value.toUpperCase().replace(/[^A-Z0-9]/g, '').replace(/^[0-9]+/, '').slice(0, 6);
    });
    panel.querySelector('[data-mr-preview]')?.addEventListener('click', async () => {
      if (!prefix.value) { prefix.focus(); return; }
      const next = await loadPlan(doctype, prefix.value, length.value);
      if (next) { plan = next; paint(); }
    });
    const detail = panel.querySelector('[data-mr-detail]');
    detail.addEventListener('click', () => guardAgainstDoubleSubmit(detail, 'Reading...', async () => {
      const full = await loadPlan(doctype, prefix?.value || '', length?.value || 0, { detail: true });
      if (!full) return;
      const touches = full.touches || [];
      panel.querySelector('[data-mr-touches]').innerHTML = `
        <table class="s-16949d">
          <thead><tr><th>Table</th><th>Column</th><th class="num">Rows</th></tr></thead>
          <tbody>${touches.map(t => `<tr><td>${esc(t.table)}</td><td>${esc(t.column)}</td><td class="num">${esc(t.rows)}</td></tr>`).join('')}</tbody>
        </table>`;
      detail.remove();
    }));
    const apply = panel.querySelector('[data-mr-apply]');
    apply.addEventListener('click', () => guardAgainstDoubleSubmit(apply, 'Renumbering...', async () => {
      const format = plan.configured ? {} : { prefix: prefix.value, length: Number(length.value) };
      if (!plan.configured && !format.prefix) { prefix.focus(); return; }
      const ack = panel.querySelector('[data-mr-ack]');
      if (ack && !ack.checked) { ack.focus(); await showCustomAlert('Tick the box under the warning first.', 'Clean old codes'); return; }
      if (ack) format.acknowledge_warning = true;
      const ok = await showCustomConfirm(
        `Renumber ${n} ${label} record${n === 1 ? '' : 's'} to ${plan.configured ? plan.prefix : format.prefix} codes? Every record that points at them is updated in the same step.`,
        'Clean old codes');
      if (!ok) return;
      const res = await apiFetch(`/api/v1/master-codes/${encodeURIComponent(doctype)}/clean`, { method: 'POST', body: JSON.stringify(format) });
      if (!res) return;
      if (!res.ok) { await showApiError(res, 'The codes were not changed.'); return; }
      const done = await res.json();
      showToast(`${(done.rows || []).length} ${label} code${(done.rows || []).length === 1 ? '' : 's'} renumbered.`, { variant: 'success' });
      renderView('doctype-table');
    }));
  };
  paint();
}
