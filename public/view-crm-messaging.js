// Stage 101.3 / 101.4 / 101.9: the CRM's messaging pages - templates,
// campaigns, journeys, messages and the CRM settings (senders, lifecycle,
// appointment hours). Loaded by view-crm.js; its helpers (crmApi, crmFrame,
// crmTable ...) are globals published by the shell's loader.

const CH = ['WhatsApp', 'SMS', 'Email'];

export async function crmRenderTab(tab, host, st) {
  const pages = { templates: crmTemplates, campaigns: crmCampaigns, journeys: crmJourneys, messages: crmMessages, settings: crmSettings };
  return pages[tab](host, st);
}

// --- templates ---------------------------------------------------------------

async function crmTemplates(host, st) {
  if (st.page === 'edit') return crmTemplateEdit(host, st.params.id);
  const body = crmFrame(host, { title: 'Templates', actions: crmMeta.can_manage ? '<button class="btn btn-primary" type="button" id="tp-new">+ New template</button>' : '' });
  host.querySelector('#tp-new')?.addEventListener('click', () => crmGo('templates', 'edit', {}));
  const r = await crmApi('templates');
  if (!r) return;
  body.innerHTML = crmTable([
    { label: 'Template', html: t => `<a href="#" data-tp="${crmEsc(t.id)}">${crmEsc(t.name)}</a>` }, { label: 'Channel', key: 'channel' },
    { label: 'For', html: t => t.purpose === 'marketing' ? 'Offers' : 'Service' }, { label: 'Language', key: 'language' }, { label: 'Version', key: 'current_version', num: true },
    { label: 'Ready to send', html: t => t.ready ? crmChip('Ready', 'ok') : `${crmChip('Not yet', 'warn')}<small class="crm-reason">${crmEsc(t.not_ready)}</small>` },
    { label: 'Status', key: 'status' }], r.templates, { empty: 'No templates yet. Journeys > Add the ready-made journeys creates one for each, or make your own.' });
  body.querySelectorAll('[data-tp]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('templates', 'edit', { id: a.dataset.tp }); }));
}

async function crmTemplateEdit(host, id) {
  let t = { channel: 'WhatsApp', purpose: 'marketing', language: 'en', status: 'Active', current: { body: '', subject: '' } };
  if (id) { const r = await crmApi('templates/' + encodeURIComponent(id)); if (!r) return; t = r; }
  const cur = t.current || {};
  const body = crmFrame(host, { title: id ? t.name : 'New template', back: () => crmGo('templates'), backLabel: 'Templates' });
  const vars = crmMeta.template_variables || [];
  body.innerHTML = `<div class="crm-grid crm-grid-seg"><form class="crm-form" id="tp-form">
      <div class="crm-form-row">${crmField('tp-name', 'Name', crmInput('tp-name', t.name || '', 'required'))}
      ${crmField('tp-ch', 'Channel', crmSelect('tp-ch', CH, t.channel, id ? 'disabled' : ''))}
      ${crmField('tp-pu', 'For', crmSelect('tp-pu', [['marketing', 'Offers (needs consent)'], ['service', 'Service (bills, reminders, feedback)']], t.purpose))}
      ${crmField('tp-lang', 'Language', crmInput('tp-lang', t.language || 'en'))}</div>
      <div id="tp-subj-row">${crmField('tp-subj', 'Email subject', crmInput('tp-subj', cur.subject || ''))}</div>
      ${crmField('tp-body', 'Message', `<textarea class="form-input" id="tp-body" rows="6" maxlength="4000">${crmEsc(cur.body || '')}</textarea>`)}
      <div class="crm-vars"><span class="crm-small">Insert:</span> ${vars.map(v => `<button type="button" class="crm-var" data-var="${crmEsc(v.Name)}" title="${crmEsc(v.Label)}">${crmEsc(v.Label)}</button>`).join('')}
      <p class="text-muted crm-small">A value like {{customer.first_name|there}} uses "there" when the name is blank. Without a fallback, a blank value stops the message (TEMPLATE_DATA_MISSING) instead of sending a gap.</p></div>
      <div id="tp-dlt-row">${crmField('tp-dlt', 'DLT template ID (needed for SMS to Indian numbers; the text must match the registered template)', crmInput('tp-dlt', cur.dlt_template_id || ''))}</div>
      <div id="tp-wa-row" class="crm-form-row">${crmField('tp-wan', 'WhatsApp template name (as approved in WhatsApp Manager)', crmInput('tp-wan', cur.wa_template_name || ''))}
        ${crmField('tp-wal', 'WhatsApp language code', crmInput('tp-wal', cur.wa_language || '', 'placeholder="en"'))}</div>
      ${crmField('tp-st', 'Status', crmSelect('tp-st', ['Active', 'Inactive'], t.status))}
      ${crmMeta.can_manage ? `<div class="crm-form-actions"><button class="btn btn-primary" type="submit">Save</button>
        ${id && t.channel === 'WhatsApp' ? '<button class="btn btn-outline" type="button" id="tp-sync">Check approval with WhatsApp</button>' : ''}</div>` : ''}
      ${id ? `<p class="text-muted crm-small">Version ${t.current_version}${cur.wa_status ? ` · WhatsApp: ${crmEsc(cur.wa_status)}` : ''}. Changing the text makes a new version; campaigns and journeys keep the version they use.</p>` : ''}
    </form><aside class="crm-aside"><h3 class="crm-section">Preview</h3>
      <div class="crm-form-row"><input class="form-input" id="tp-pc" placeholder="Customer (blank = sample values)"><button class="btn btn-outline btn-sm" type="button" id="tp-prev">Preview</button></div>
      <div id="tp-pout" class="crm-preview"></div></aside></div>`;
  const pick = crmCustomerPicker(body.querySelector('#tp-pc'));
  const sync = () => {
    const ch = crmVal(body, 'tp-ch');
    body.querySelector('#tp-subj-row').hidden = ch !== 'Email';
    body.querySelector('#tp-dlt-row').hidden = ch !== 'SMS';
    body.querySelector('#tp-wa-row').hidden = ch !== 'WhatsApp';
  };
  sync();
  body.querySelector('#tp-ch').addEventListener('change', sync);
  const ta = body.querySelector('#tp-body');
  body.querySelectorAll('[data-var]').forEach(b => b.addEventListener('click', () => {
    const ins = `{{${b.dataset.var}}}`;
    const s = ta.selectionStart || ta.value.length;
    ta.value = ta.value.slice(0, s) + ins + ta.value.slice(ta.selectionEnd || s);
    ta.focus();
  }));
  body.querySelector('#tp-prev').addEventListener('click', async () => {
    const out = body.querySelector('#tp-pout');
    if (!id) { out.innerHTML = '<p class="text-muted">Save the template first, then preview it.</p>'; return; }
    const r = await crmApi(`templates/${encodeURIComponent(id)}/preview`, { method: 'POST', body: { customer_id: pick() } });
    if (r) out.innerHTML = `${r.subject ? `<p><b>${crmEsc(r.subject)}</b></p>` : ''}<p class="crm-bubble">${crmEsc(r.body).replace(/\n/g, '<br>')}</p>
      <p class="text-muted crm-small">${t.channel === 'SMS' ? `${r.sms_parts} SMS part(s)${r.sms_unicode ? ' (non-English letters: 70 per part)' : ''}. ` : ''}${r.missing.length ? `<b>Blank - this message would not go:</b> ${r.missing.map(crmEsc).join(', ')}` : ''}${r.sample ? ' Sample values.' : ''}</p>`;
  });
  body.querySelector('#tp-sync')?.addEventListener('click', async () => {
    const r = await crmApi(`templates/${encodeURIComponent(id)}/sync`, { method: 'POST', body: {} });
    if (r) { showToast(`WhatsApp says: ${r.current.wa_status || 'not found'}.`); crmGo('templates', 'edit', { id }); }
  });
  body.querySelector('#tp-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const payload = { name: crmVal(body, 'tp-name'), channel: crmVal(body, 'tp-ch'), purpose: crmVal(body, 'tp-pu'), language: crmVal(body, 'tp-lang'), subject: crmVal(body, 'tp-subj'),
      body: body.querySelector('#tp-body').value, dlt_template_id: crmVal(body, 'tp-dlt'), wa_template_name: crmVal(body, 'tp-wan'), wa_language: crmVal(body, 'tp-wal'), status: crmVal(body, 'tp-st') };
    const r = await crmApi(id ? 'templates/' + encodeURIComponent(id) : 'templates', { method: id ? 'PUT' : 'POST', body: payload });
    if (r) { showToast(`Saved - version ${r.current_version}.`, { variant: 'success' }); crmGo('templates', 'edit', { id: r.id }); }
  });
}

// --- campaigns -------------------------------------------------------------------

async function crmCampaigns(host, st) {
  if (st.page === 'edit') return crmCampaignEdit(host, st.params.id);
  const body = crmFrame(host, { title: 'Campaigns', actions: crmMeta.can_manage ? '<button class="btn btn-primary" type="button" id="cm-new">+ New campaign</button>' : '' });
  host.querySelector('#cm-new')?.addEventListener('click', () => crmGo('campaigns', 'edit', {}));
  const r = await crmApi('campaigns');
  if (!r) return;
  body.innerHTML = crmTable([
    { label: 'Campaign', html: c => `<a href="#" data-cm="${crmEsc(c.id)}">${crmEsc(c.data.name || c.id)}</a>${c.legacy ? ' ' + crmChip('Older trigger campaign', 'muted') : ''}` },
    { label: 'Channel', html: c => crmEsc(c.data.channel) }, { label: 'Status', html: c => crmStatus(c.status) },
    { label: 'Last run', html: c => { const run = (c.runs || [])[0]; return run ? `${crmStatus(run.status)} ${crmDate(run.started_at || run.scheduled_at, true)}` : ''; } },
    { label: 'Matched', num: true, html: c => { const run = (c.runs || [])[0]; return run ? (run.counts.matched || 0) : ''; } }],
    r.campaigns, { empty: 'No campaigns yet. A campaign sends one approved message to a segment.' });
  body.querySelectorAll('[data-cm]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('campaigns', 'edit', { id: a.dataset.cm }); }));
}

async function crmCampaignEdit(host, id) {
  let c = { status: 'Draft', data: { channel: 'WhatsApp', goal_days: 7, holdout_pct: 0 }, runs: [] };
  if (id) { const r = await crmApi('campaigns/' + encodeURIComponent(id)); if (!r) return; c = r; }
  const d = c.data;
  const editable = crmMeta.can_manage && (c.status === 'Draft' || c.status === 'Rejected' || c.status === 'Pending Approval' || c.status === 'Approved') && !c.legacy;
  const body = crmFrame(host, { title: id ? d.name : 'New campaign', back: () => crmGo('campaigns'), backLabel: 'Campaigns', actions: id ? crmStatus(c.status) : '' });
  if (c.legacy) {
    body.innerHTML = `<div class="crm-note">An older trigger campaign (${crmEsc(d.trigger_type)}), kept as it was. Its replacement is the ready-made Birthday and Win back journeys (Journeys).</div>`;
    return;
  }
  const [segs, tpls] = await Promise.all([crmApi('segments'), crmApi('templates')]);
  const v = d.voucher || {};
  body.innerHTML = `<form class="crm-form" id="cm-form">
    <div class="crm-form-row">${crmField('cm-name', 'Name', crmInput('cm-name', d.name || '', 'required'))}
      ${crmField('cm-ch', 'Send by', crmSelect('cm-ch', CH, d.channel))}</div>
    <div class="crm-form-row">${crmField('cm-seg', 'To segment', crmSelect('cm-seg', [['', 'Choose a segment']].concat(((segs || {}).segments || []).map(s => [s.id, `${s.name} (v${s.current_version})`])), d.segment_id || ''))}
      ${crmField('cm-tpl', 'Message', `<select class="form-input" id="cm-tpl"></select>`)}</div>
    <div class="crm-form-row">${crmField('cm-at', 'Start (blank = as soon as approved)', crmInput('cm-at', (d.schedule_at || '').replace(' ', 'T'), 'type="datetime-local"'))}
      ${crmField('cm-end', 'Stop sending at', crmInput('cm-end', (d.ends_at || '').replace(' ', 'T'), 'type="datetime-local"'))}</div>
    <div class="crm-form-row">${crmField('cm-bud', 'Budget (blank = none)', crmInput('cm-bud', d.budget || '', 'inputmode="decimal"'))}
      ${crmField('cm-goal', 'Count a bill within (days)', crmInput('cm-goal', d.goal_days || 7, 'inputmode="numeric"'))}
      ${crmField('cm-hold', 'Hold-out group (% not sent, to measure lift)', crmInput('cm-hold', d.holdout_pct || 0, 'inputmode="numeric"'))}</div>
    <label class="crm-check"><input type="checkbox" id="cm-skip" ${d.skip_if_bought ? 'checked' : ''}> Don't send to someone who already bought after the campaign started</label>
    <fieldset class="crm-fieldset"><legend>A voucher for each customer (optional)</legend><div class="crm-form-row">
      ${crmField('cm-vt', 'Type', crmSelect('cm-vt', [['', 'No voucher'], ['Percentage', '% off'], ['Flat', 'Amount off']], v.discount_value ? v.discount_type : ''))}
      ${crmField('cm-vv', 'Value', crmInput('cm-vv', v.discount_value || '', 'inputmode="numeric"'))}
      ${crmField('cm-vd', 'Valid for (days)', crmInput('cm-vd', v.valid_days || 30, 'inputmode="numeric"'))}
      ${crmField('cm-vc', 'Use at', crmSelect('cm-vc', [['Any', 'Store and web shop'], ['POS', 'Store only'], ['Online', 'Web shop only']], v.channel || 'Any'))}</div>
      <p class="text-muted crm-small">Each customer gets their own single-use code ({{voucher.code}} in the message).</p></fieldset>
    ${editable ? `<div class="crm-form-actions"><button class="btn btn-primary" type="submit">Save draft</button></div>` : ''}
    ${c.status === 'Pending Approval' || c.status === 'Approved' ? '<p class="text-muted crm-small">Changing anything sends it back to Draft - it must be approved again.</p>' : ''}
  </form>
  ${id ? `<div class="crm-actions-row">
    <button class="btn btn-outline" type="button" id="cm-check">Pre-send check</button>
    <span class="crm-test"><input class="form-input" id="cm-tc" placeholder="Test customer (on the test list)"><button class="btn btn-outline" type="button" id="cm-test">Send a test</button></span>
    ${(c.status === 'Draft' || c.status === 'Rejected') && crmMeta.can_manage ? '<button class="btn btn-primary" type="button" id="cm-submit">Submit for approval</button>' : ''}</div>
    <div id="cm-out"></div>
    <h3 class="crm-section">Runs</h3><div id="cm-runs">${(c.runs || []).map(r => `<div class="crm-run" data-run="${crmEsc(r.id)}">
      <div class="crm-run-head">${crmStatus(r.status)} ${r.test ? crmChip('Test') : ''} <span>${crmDate(r.started_at || r.scheduled_at, true)}</span>
      ${['Running', 'Scheduled'].includes(r.status) ? '<button class="btn btn-outline btn-sm" data-act="pause">Pause</button>' : ''}
      ${r.status === 'Paused' ? '<button class="btn btn-outline btn-sm" data-act="resume">Resume</button>' : ''}
      ${['Running', 'Paused', 'Scheduled'].includes(r.status) ? '<button class="btn btn-outline btn-sm" data-act="stop">Stop</button>' : ''}
      <button class="btn btn-outline btn-sm" data-act="results">Results</button></div><div class="crm-run-out"></div></div>`).join('') || '<p class="text-muted">Approval schedules the first run.</p>'}</div>
    ${(c.approval_log || []).length ? `<h3 class="crm-section">Approval</h3>${crmTable([{ label: 'When', html: a => crmDate(a.created_at || a.timestamp, true) }, { label: 'What', key: 'action' }, { label: 'Who', html: a => crmEsc(a.actor_user_id || a.actor || '') }, { label: 'Note', key: 'comment' }], c.approval_log)}` : ''}` : ''}`;
  const tplSel = body.querySelector('#cm-tpl');
  const fillTpl = () => {
    const ch = crmVal(body, 'cm-ch');
    const opts = ((tpls || {}).templates || []).filter(t => t.channel === ch && t.purpose === 'marketing');
    tplSel.innerHTML = '<option value="">Choose a message</option>' + opts.map(t => `<option value="${crmEsc(t.id)}"${t.id === d.template_id ? ' selected' : ''}>${crmEsc(t.name)} (v${t.current_version})${t.ready ? '' : ' - not ready'}</option>`).join('');
  };
  fillTpl();
  body.querySelector('#cm-ch').addEventListener('change', fillTpl);
  if (!editable) body.querySelectorAll('#cm-form input, #cm-form select').forEach(el => { el.disabled = true; });
  body.querySelector('#cm-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const payload = { name: crmVal(body, 'cm-name'), channel: crmVal(body, 'cm-ch'), segment_id: crmVal(body, 'cm-seg'), template_id: crmVal(body, 'cm-tpl'),
      schedule_at: crmVal(body, 'cm-at'), ends_at: crmVal(body, 'cm-end'), budget: Number(crmVal(body, 'cm-bud')) || 0, goal_days: Number(crmVal(body, 'cm-goal')) || 7,
      holdout_pct: Number(crmVal(body, 'cm-hold')) || 0, skip_if_bought: crmVal(body, 'cm-skip') };
    if (crmVal(body, 'cm-vt')) payload.voucher = { discount_type: crmVal(body, 'cm-vt'), discount_value: Number(crmVal(body, 'cm-vv')) || 0, valid_days: Number(crmVal(body, 'cm-vd')) || 30, channel: crmVal(body, 'cm-vc') };
    const r = await crmApi(id ? 'campaigns/' + encodeURIComponent(id) : 'campaigns', { method: id ? 'PUT' : 'POST', body: payload });
    if (r) { showToast('Saved as a draft.', { variant: 'success' }); crmGo('campaigns', 'edit', { id: r.id }); }
  });
  if (!id) return;
  const out = body.querySelector('#cm-out');
  const testPick = crmCustomerPicker(body.querySelector('#cm-tc'));
  body.querySelector('#cm-check').addEventListener('click', async () => {
    const p = await crmApi(`campaigns/${encodeURIComponent(id)}/preview`);
    if (!p) return;
    const s = p.segment || {};
    out.innerHTML = `<div class="crm-note">${p.problems.length ? `<p><b>Not ready:</b></p><ul>${p.problems.map(x => `<li>${crmEsc(x)}</li>`).join('')}</ul>` : '<p><b>Ready to submit.</b></p>'}
      <div class="crm-waterfall"><div class="crm-wf"><span>In the segment</span><b>${s.matched ?? 0}</b></div>
      <div class="crm-wf crm-wf-minus"><span>No consent</span><b>- ${s.no_consent ?? 0}</b></div><div class="crm-wf crm-wf-minus"><span>No contact</span><b>- ${s.no_contact ?? 0}</b></div>
      <div class="crm-wf crm-wf-minus"><span>Hold-out (${p.holdout_pct}%)</span><b>- ${p.held_out_estimate}</b></div>
      <div class="crm-wf crm-wf-total"><span>Will be sent (before limits and quiet hours)</span><b>${p.final}</b></div></div>
      <p class="crm-small">Estimated cost ${crmMoney(p.estimate_total)} (${crmMoney(p.estimate_each)} each${p.sms_parts ? `, ${p.sms_parts} SMS part(s)` : ''})${p.budget ? ` against a budget of ${crmMoney(p.budget)}` : ''}.${p.over_budget ? ' <b>Over budget - the last messages will not go.</b>' : ''}</p></div>`;
  });
  body.querySelector('#cm-test').addEventListener('click', async () => {
    const r = await crmApi(`campaigns/${encodeURIComponent(id)}/test`, { method: 'POST', body: { customer_id: testPick() } });
    if (r) out.innerHTML = `<div class="crm-note">Test: ${crmStatus(r.status)} ${crmWhy(r)}${r.body ? `<p class="crm-bubble">${crmEsc(r.body)}</p>` : ''}</div>`;
  });
  body.querySelector('#cm-submit')?.addEventListener('click', async () => {
    const r = await crmApi(`campaigns/${encodeURIComponent(id)}/submit`, { method: 'POST', body: {} });
    if (r) { showToast('Sent for approval. It appears under Approvals for the approver.', { variant: 'success' }); crmGo('campaigns', 'edit', { id }); }
  });
  body.querySelectorAll('[data-run]').forEach(box => box.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-act]');
    if (!b) return;
    const runID = box.dataset.run, act = b.dataset.act;
    if (act === 'results') {
      const r = await crmApi(`campaign-runs/${encodeURIComponent(runID)}/results`);
      if (!r) return;
      const supp = Object.entries(r.suppressed || {}).map(([k, n]) => `${crmEsc((crmMeta.reasons || {})[k] || k)}: ${n}`).join('<br>');
      const blank = (n) => (n == null ? '<span title="Not reported by this channel">-</span>' : n);
      box.querySelector('.crm-run-out').innerHTML = `<div class="crm-tiles crm-tiles-sm">
        <div class="crm-tile"><span class="crm-tile-value">${r.matched}</span><span class="crm-tile-label">Matched</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${r.sent}</span><span class="crm-tile-label">Sent</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${blank(r.delivered)}</span><span class="crm-tile-label">Delivered</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${blank(r.read)}</span><span class="crm-tile-label">Read</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${r.failed} / ${r.unknown}</span><span class="crm-tile-label">Failed / unknown</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${r.vouchers_used} of ${r.vouchers_issued}</span><span class="crm-tile-label">Vouchers used</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${r.bills_credited} · ${crmMoney(r.sales_credited)}</span><span class="crm-tile-label">Bills credited (${r.attribution_days} days)</span></div>
        <div class="crm-tile"><span class="crm-tile-value">${r.cost_actual == null ? crmMoney(r.cost_estimate) + ' est.' : crmMoney(r.cost_actual)}</span><span class="crm-tile-label">Cost</span></div></div>
        ${supp ? `<p class="crm-small"><b>Not sent (${r.held_out} held out on purpose):</b><br>${supp}</p>` : ''}
        ${r.lift ? `<p class="crm-small"><b>Lift:</b> ${crmEsc(r.lift.verdict)} (messaged ${r.lift.treated_rate}% bought, hold-out ${r.lift.control_rate}%)</p>` : ''}
        <p class="text-muted crm-small">${crmEsc(r.date_basis)}. Time zone ${crmEsc(r.time_zone)}.</p>`;
      return;
    }
    if (act === 'stop' && !await showCustomConfirm('Stop this run? Messages not yet sent are cancelled.', 'Stop the campaign')) return;
    if (await crmApi(`campaign-runs/${encodeURIComponent(runID)}/${act}`, { method: 'POST', body: {} })) crmGo('campaigns', 'edit', { id });
  }));
}

// --- journeys -----------------------------------------------------------------

async function crmJourneys(host, st) {
  if (st.page === 'edit') return crmJourneyEdit(host, st.params.id);
  const body = crmFrame(host, { title: 'Journeys', actions: crmMeta.can_manage ? `<button class="btn btn-outline" type="button" id="jr-ready">Add the ready-made journeys</button>
    <button class="btn btn-primary" type="button" id="jr-new">+ New journey</button>` : '' });
  host.querySelector('#jr-new')?.addEventListener('click', () => crmGo('journeys', 'edit', {}));
  host.querySelector('#jr-ready')?.addEventListener('click', async () => {
    if (await crmApi('journeys/ready-made', { method: 'POST', body: {} })) { showToast('Added. Each is off until you switch it on.', { variant: 'success' }); crmGo('journeys'); }
  });
  const r = await crmApi('journeys');
  if (!r) return;
  body.innerHTML = crmTable([
    { label: 'Journey', html: j => `<a href="#" data-jr="${crmEsc(j.id)}">${crmEsc(j.name)}</a>${j.unpublished ? ' ' + crmChip('Unpublished changes', 'warn') : ''}<small class="crm-sub">${crmEsc(j.description || '')}</small>` },
    { label: 'On', html: j => j.enabled ? crmChip('On', 'ok') : crmChip('Off', 'muted') }, { label: 'Version', key: 'current_version', num: true },
    { label: 'In it now', key: 'live', num: true }, { label: 'Finished', key: 'completed', num: true }, { label: 'Reached the goal', key: 'exited_goal', num: true }, { label: 'Messages', key: 'sent', num: true }],
    r.journeys, { empty: 'No journeys yet. "Add the ready-made journeys" adds welcome, birthday, win-back and more - each off until you switch it on.' });
  body.querySelectorAll('[data-jr]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('journeys', 'edit', { id: a.dataset.jr }); }));
}

const crmStepTypes = [['wait', 'Wait'], ['wait_event', 'Wait for something to happen'], ['check', 'Check (yes / no)'], ['send', 'Send a message'], ['task', 'Create a task for staff'], ['end', 'End']];

async function crmJourneyEdit(host, id) {
  let j = { name: '', description: '', draft: { trigger: { type: 'event', event: 'bill_paid' }, cooldown_days: 0, steps: [{ type: 'wait', hours: 2 }] } };
  if (id) { const r = await crmApi('journeys/' + encodeURIComponent(id)); if (!r) return; j = r; }
  const def = JSON.parse(JSON.stringify(j.draft || j.published || { trigger: { type: 'event' }, steps: [] }));
  const tpls = ((await crmApi('templates')) || {}).templates || [];
  const segs = ((await crmApi('segments')) || {}).segments || [];
  const events = (crmMeta.events || []).map(e => [e.Key, e.Label]);
  const body = crmFrame(host, { title: id ? j.name : 'New journey', back: () => crmGo('journeys'), backLabel: 'Journeys', actions: id ? (j.enabled ? crmChip('On', 'ok') : crmChip('Off', 'muted')) : '' });
  const fieldsSel = (crmMeta.segment_fields || []).filter(f => !['in_list'].includes(f.Field)).map(f => [f.Field, f.Label]);
  const stepHTML = (s, i) => {
    let p = '';
    if (s.type === 'wait') p = crmField(`st-h-${i}`, 'Hours', crmInput(`st-h-${i}`, s.hours || 1, `data-sk="hours" data-i="${i}" inputmode="numeric"`));
    if (s.type === 'wait_event') p = crmField(`st-e-${i}`, 'For', crmSelect(`st-e-${i}`, events, s.event, `data-sk="event" data-i="${i}"`)) +
      crmField(`st-h-${i}`, 'Up to hours', crmInput(`st-h-${i}`, s.hours || 24, `data-sk="hours" data-i="${i}" inputmode="numeric"`));
    if (s.type === 'check') { const r = s.rule || { field: 'visits', cmp: '>=', value: 2 }; s.rule = r;
      p = crmField(`st-cf-${i}`, 'If', crmSelect(`st-cf-${i}`, fieldsSel, r.field, `data-rk="field" data-i="${i}"`)) +
        crmField(`st-cc-${i}`, '', crmSelect(`st-cc-${i}`, [['>=', 'at least'], ['<=', 'at most'], ['=', 'is'], ['!=', 'is not']], r.cmp || '>=', `data-rk="cmp" data-i="${i}"`)) +
        crmField(`st-cv-${i}`, '', crmInput(`st-cv-${i}`, r.value ?? '', `data-rk="value" data-i="${i}"`)); }
    if (s.type === 'send') p = crmField(`st-ch-${i}`, 'By', crmSelect(`st-ch-${i}`, CH, s.channel || 'WhatsApp', `data-sk="channel" data-i="${i}"`)) +
      crmField(`st-t-${i}`, 'Message', crmSelect(`st-t-${i}`, [['', 'Choose']].concat(tpls.filter(t => t.channel === (s.channel || 'WhatsApp')).map(t => [t.id, `${t.name}${t.ready ? '' : ' (not ready)'}`])), s.template_id || '', `data-sk="template_id" data-i="${i}"`)) +
      crmField(`st-p-${i}`, 'For', crmSelect(`st-p-${i}`, [['marketing', 'Offers'], ['service', 'Service']], s.purpose || 'marketing', `data-sk="purpose" data-i="${i}"`)) +
      crmField(`st-vv-${i}`, 'Voucher % off (blank = none)', crmInput(`st-vv-${i}`, (s.voucher || {}).discount_value || '', `data-sk="voucher_pct" data-i="${i}" inputmode="numeric"`));
    if (s.type === 'task') p = crmField(`st-ti-${i}`, 'Task', crmInput(`st-ti-${i}`, s.title || '', `data-sk="title" data-i="${i}"`)) +
      crmField(`st-to-${i}`, 'For', crmSelect(`st-to-${i}`, [['store', 'Anyone at the store'], ['preferred_salesman', 'Their salesman']], s.owner || 'store', `data-sk="owner" data-i="${i}"`)) +
      crmField(`st-td-${i}`, 'Due in hours', crmInput(`st-td-${i}`, s.due_hours || 24, `data-sk="due_hours" data-i="${i}" inputmode="numeric"`));
    const branch = (s.type === 'check' || s.type === 'wait_event') ? `<div class="crm-form-row">
      ${crmField(`st-y-${i}`, s.type === 'check' ? 'If yes, go to step' : 'If it happens, go to step', crmSelect(`st-y-${i}`, [[0, 'the next step'], [-1, 'end']].concat(def.steps.map((_, k) => [k + 1, `step ${k + 1}`]).filter(x => x[0] !== i + 1)), s.yes || 0, `data-sk="yes" data-i="${i}"`))}
      ${crmField(`st-n-${i}`, s.type === 'check' ? 'If no, go to step' : 'If not, go to step', crmSelect(`st-n-${i}`, [[0, 'the next step'], [-1, 'end']].concat(def.steps.map((_, k) => [k + 1, `step ${k + 1}`]).filter(x => x[0] !== i + 1)), s.no || 0, `data-sk="no" data-i="${i}"`))}</div>` : '';
    return `<li class="crm-step"><div class="crm-step-head"><b>Step ${i + 1}</b>${crmSelect(`st-ty-${i}`, crmStepTypes, s.type, `data-sk="type" data-i="${i}"`)}
      <button type="button" class="action-btn" data-up="${i}" ${i === 0 ? 'disabled' : ''}>Up</button><button type="button" class="action-btn" data-rm="${i}">Remove</button></div>
      <div class="crm-form-row">${p}</div>${branch}${(j.step_counts || {})[i + 1] ? `<small class="crm-sub">${j.step_counts[i + 1]} customer(s) here now</small>` : ''}</li>`;
  };
  const paint = () => {
    const t = def.trigger || (def.trigger = { type: 'event' });
    body.innerHTML = `<form class="crm-form" id="jr-form">
      <div class="crm-form-row">${crmField('jr-name', 'Name', crmInput('jr-name', j.name || ''))}${crmField('jr-desc', 'About', crmInput('jr-desc', j.description || ''))}</div>
      <fieldset class="crm-fieldset"><legend>Starts when</legend><div class="crm-form-row">
        ${crmField('jr-tt', 'Type', crmSelect('jr-tt', [['event', 'Something happens'], ['date', 'A date comes round'], ['inactive', 'Not seen for some days'], ['segment', 'They are in a segment']], t.type))}
        ${t.type === 'event' ? crmField('jr-te', 'What', crmSelect('jr-te', events, t.event)) + (t.event === 'bill_paid' ? `<label class="crm-check"><input type="checkbox" id="jr-tf" ${t.first_bill_only ? 'checked' : ''}> First bill only</label>` : '') +
          crmField('jr-ts', 'Ignore events older than hours (0 = never)', crmInput('jr-ts', t.stale_hours || 0, 'inputmode="numeric"')) : ''}
        ${t.type === 'date' ? crmField('jr-td', 'Date', crmSelect('jr-td', [['birthday', 'Birthday'], ['anniversary', 'Anniversary'], ['points_expiring', 'Points expiring']], t.date || 'birthday')) +
          crmField('jr-tb', 'Days before', crmInput('jr-tb', t.days_before || 0, 'inputmode="numeric"')) : ''}
        ${t.type === 'inactive' ? crmField('jr-tn', 'Days since the last bill', crmInput('jr-tn', t.days || 90, 'inputmode="numeric"')) : ''}
        ${t.type === 'segment' ? crmField('jr-tg', 'Segment', crmSelect('jr-tg', [['', 'Choose']].concat(segs.map(s => [s.id, s.name])), t.segment_id || '')) : ''}
      </div><div class="crm-form-row">
        ${crmField('jr-goal', 'Leave the journey early when', crmSelect('jr-goal', [['', 'Never (run every step)']].concat(events), (def.goal || {}).event || ''))}
        ${crmField('jr-cool', 'Do not start again for (days)', crmInput('jr-cool', def.cooldown_days || 0, 'inputmode="numeric"'))}
        ${t.event === 'cart_abandoned' ? `<label class="crm-check"><input type="checkbox" id="jr-cart" ${def.per_cart ? 'checked' : ''}> One run per cart</label>` : ''}</div></fieldset>
      <ol class="crm-steps">${def.steps.map(stepHTML).join('')}</ol>
      <button type="button" class="btn btn-outline btn-sm" id="jr-add">+ Step</button>
      ${crmMeta.can_manage ? `<div class="crm-form-actions"><button class="btn btn-outline" type="submit">Save draft</button>
        ${id ? '<button class="btn btn-primary" type="button" id="jr-pub">Publish</button>' : ''}
        ${id && j.current_version ? `<button class="btn btn-outline" type="button" id="jr-onoff">${j.enabled ? 'Switch off' : 'Switch on'}</button>` : ''}</div>` : ''}
      <p class="text-muted crm-small">${id ? `Published version ${j.current_version || '-'}${j.unpublished ? '; the draft has changes - publish them for new customers' : ''}. ` : ''}Customers already in a journey finish the version they started. A customer is in a journey once at a time.</p>
    </form><div id="jr-runs"></div>`;
    wire();
  };
  const readTrigger = () => {
    const t = { type: crmVal(body, 'jr-tt') };
    if (t.type === 'event') { t.event = crmVal(body, 'jr-te') || 'bill_paid'; t.first_bill_only = !!crmVal(body, 'jr-tf'); t.stale_hours = Number(crmVal(body, 'jr-ts')) || 0; }
    if (t.type === 'date') { t.date = crmVal(body, 'jr-td') || 'birthday'; t.days_before = Number(crmVal(body, 'jr-tb')) || 0; }
    if (t.type === 'inactive') t.days = Number(crmVal(body, 'jr-tn')) || 90;
    if (t.type === 'segment') t.segment_id = crmVal(body, 'jr-tg');
    def.trigger = t;
    const goal = crmVal(body, 'jr-goal');
    def.goal = goal ? { event: goal } : null;
    def.cooldown_days = Number(crmVal(body, 'jr-cool')) || 0;
    def.per_cart = !!crmVal(body, 'jr-cart');
    j.name = crmVal(body, 'jr-name'); j.description = crmVal(body, 'jr-desc');
  };
  const wire = () => {
    ['jr-tt', 'jr-te'].forEach(x => body.querySelector('#' + x)?.addEventListener('change', () => { readTrigger(); paint(); }));
    body.querySelector('#jr-form').addEventListener('change', (e) => {
      const el = e.target, i = Number(el.dataset.i);
      if (el.dataset.sk) {
        const s = def.steps[i], k = el.dataset.sk;
        if (k === 'type') { def.steps[i] = { type: el.value }; readTrigger(); paint(); return; }
        if (k === 'voucher_pct') { s.voucher = el.value ? { discount_type: 'Percentage', discount_value: Number(el.value), valid_days: 14, channel: 'Any' } : null; return; }
        s[k] = ['hours', 'due_hours', 'yes', 'no'].includes(k) ? Number(el.value) : el.value;
        if (k === 'channel') { readTrigger(); paint(); }
      }
      if (el.dataset.rk) { const r = def.steps[i].rule; r[el.dataset.rk] = el.dataset.rk === 'value' && /^-?\d+(\.\d+)?$/.test(el.value) ? Number(el.value) : el.value; }
    });
    body.querySelector('#jr-add').addEventListener('click', () => { readTrigger(); def.steps.push({ type: 'send', channel: 'WhatsApp', purpose: 'marketing' }); paint(); });
    body.querySelectorAll('[data-rm]').forEach(b => b.addEventListener('click', () => { readTrigger(); def.steps.splice(Number(b.dataset.rm), 1); paint(); }));
    body.querySelectorAll('[data-up]').forEach(b => b.addEventListener('click', () => { readTrigger(); const i = Number(b.dataset.up); [def.steps[i - 1], def.steps[i]] = [def.steps[i], def.steps[i - 1]]; paint(); }));
    const save = async () => {
      readTrigger();
      const r = await crmApi(id ? 'journeys/' + encodeURIComponent(id) : 'journeys', { method: id ? 'PUT' : 'POST', body: { name: j.name, description: j.description, definition: def } });
      return r;
    };
    body.querySelector('#jr-form').addEventListener('submit', async (e) => { e.preventDefault(); const r = await save(); if (r) { showToast('Draft saved.', { variant: 'success' }); crmGo('journeys', 'edit', { id: r.id }); } });
    body.querySelector('#jr-pub')?.addEventListener('click', async () => {
      if (!await save()) return;
      const r = await crmApi(`journeys/${encodeURIComponent(id)}/publish`, { method: 'POST', body: {} });
      if (r) { showToast(`Published - version ${r.current_version}.`, { variant: 'success' }); crmGo('journeys', 'edit', { id }); }
    });
    body.querySelector('#jr-onoff')?.addEventListener('click', async () => {
      let stopLive = false;
      if (j.enabled) stopLive = await showCustomConfirm('Also end it for the customers in it now? (Cancel keeps their runs going; only new customers stop entering.)', 'Switch off');
      const r = await crmApi(`journeys/${encodeURIComponent(id)}/switch`, { method: 'POST', body: { enabled: !j.enabled, stop_live: stopLive } });
      if (r) crmGo('journeys', 'edit', { id });
    });
  };
  paint();
  if (id) {
    const runs = await crmApi(`journeys/${encodeURIComponent(id)}/runs?limit=50`);
    if (runs) body.querySelector('#jr-runs').innerHTML = `<h3 class="crm-section">Customers in it (latest 50)</h3>` + crmTable([
      { label: 'Customer', html: r => `<a href="#" data-cust="${crmEsc(r.customer_id)}">${crmEsc(r.customer || r.customer_id)}</a>` }, { label: 'State', html: r => crmStatus(r.state) },
      { label: 'Step', key: 'step', num: true }, { label: 'Waiting until', html: r => crmDate(r.wait_until, true) }, { label: 'Why it ended', key: 'exit_reason' },
      { label: 'Started', html: r => crmDate(r.started_at, true) }, { label: 'Version', key: 'version', num: true }], runs.runs, { empty: 'Nobody has entered it yet.' });
    body.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
  }
}

// --- messages -------------------------------------------------------------------

async function crmMessages(host, st) {
  if (st.page === 'new') return crmMessageNew(host, st.params);
  const p = st.params || {};
  const body = crmFrame(host, { title: 'Messages', actions: '<button class="btn btn-primary" type="button" id="ms-new">+ New message</button>' });
  host.querySelector('#ms-new').addEventListener('click', () => crmGo('messages', 'new', {}));
  const reasons = Object.entries(crmMeta.reasons || {});
  body.innerHTML = `<div class="crm-filters">
    ${crmField('ms-st', 'Status', crmSelect('ms-st', ['', 'Queued', 'Deferred', 'Sending', 'Accepted', 'Delivered', 'Read', 'Failed', 'Unknown', 'Suppressed', 'Expired', 'Cancelled', 'HeldOut', 'Simulated'].map(x => [x, x || 'All']), p.status || ''))}
    ${crmField('ms-ch', 'Channel', crmSelect('ms-ch', [['', 'All']].concat(CH), p.channel || ''))}
    ${crmField('ms-k', 'From', crmSelect('ms-k', [['', 'All'], ['campaign', 'Campaigns'], ['journey', 'Journeys'], ['staff', 'Staff'], ['feedback', 'Feedback'], ['reminder', 'Reminders'], ['test', 'Tests'], ['auto_reply', 'Automatic replies']], p.run_kind || ''))}
    ${crmField('ms-r', 'Reason', crmSelect('ms-r', [['', 'All']].concat(reasons.map(([k, l]) => [k, l])), p.reason_code || ''))}</div><div id="ms-list"></div>`;
  const load = async () => {
    const q = new URLSearchParams({ status: crmVal(body, 'ms-st'), channel: crmVal(body, 'ms-ch'), run_kind: crmVal(body, 'ms-k'), reason_code: crmVal(body, 'ms-r'), limit: '200' });
    const r = await crmApi('messages?' + q.toString());
    if (!r) return;
    body.querySelector('#ms-list').innerHTML = crmTable([
      { label: 'Queued', html: m => crmDate(m.created_at, true) }, { label: 'Customer', html: m => `<a href="#" data-cust="${crmEsc(m.customer_id)}">${crmEsc(m.customer || m.customer_id)}</a>` },
      { label: 'From', key: 'run_kind' }, { label: 'Channel', key: 'channel' }, { label: 'For', key: 'purpose' }, { label: 'Status', html: m => crmStatus(m.status) },
      { label: 'Why', html: m => crmWhy(m) }, { label: 'To', key: 'contact' },
      { label: 'Cost', num: true, html: m => m.cost != null ? crmMoney(m.cost) : (m.cost_estimate != null ? crmMoney(m.cost_estimate) + ' est.' : '') },
      { label: '', html: m => m.status === 'Unknown' && crmMeta.can_manage ? `<button class="action-btn" data-settle="${crmEsc(m.id)}">Settle</button>` : '' }],
      r.messages, { empty: 'No messages match.' });
    body.querySelectorAll('[data-cust]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('customers', 'customer', { id: a.dataset.cust }); }));
    body.querySelectorAll('[data-settle]').forEach(b => b.addEventListener('click', async () => {
      const outcome = await showCustomPrompt('What happened? Type delivered, accepted or failed.', 'delivered', 'Settle an unknown message');
      if (!outcome) return;
      const note = await showCustomPrompt('How do you know? (for example: "Checked the provider dashboard - delivered 10:42")', '', 'Settle an unknown message');
      if (!note) return;
      if (await crmApi(`messages/${encodeURIComponent(b.dataset.settle)}/resolve`, { method: 'POST', body: { outcome, note } })) load();
    }));
  };
  body.querySelectorAll('select').forEach(s => s.addEventListener('change', load));
  await load();
}

async function crmMessageNew(host, p) {
  const body = crmFrame(host, { title: 'New message', back: () => crmGo('messages'), backLabel: 'Messages' });
  const tpls = ((await crmApi('templates')) || {}).templates || [];
  body.innerHTML = `<form class="crm-form" id="mn-form">
    ${crmField('mn-c', 'To customer', crmInput('mn-c', p.name || ''))}
    <div class="crm-form-row">${crmField('mn-ch', 'By', crmSelect('mn-ch', CH))}${crmField('mn-pu', 'For', crmSelect('mn-pu', [['service', 'Service (about their order, visit, request)'], ['marketing', 'An offer (needs their consent)']]))}</div>
    ${crmField('mn-t', 'Template', '<select class="form-input" id="mn-t"></select>')}
    ${crmField('mn-x', 'Or your own text (WhatsApp only within 24 hours of their last message; never by SMS)', '<textarea class="form-input" id="mn-x" rows="3" maxlength="2000"></textarea>')}
    <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Send</button></div></form><div id="mn-out"></div>
    <p class="text-muted crm-small">The message goes through the same rules as every CRM message: consent, contact limits for offers, quiet hours, and the test list on a test server.</p>`;
  const pick = crmCustomerPicker(body.querySelector('#mn-c'), p.customer_id);
  const fill = () => { const ch = crmVal(body, 'mn-ch'); body.querySelector('#mn-t').innerHTML = '<option value="">None - my own text</option>' + tpls.filter(t => t.channel === ch && t.status === 'Active').map(t => `<option value="${crmEsc(t.id)}">${crmEsc(t.name)}</option>`).join(''); };
  fill();
  body.querySelector('#mn-ch').addEventListener('change', fill);
  body.querySelector('#mn-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const r = await crmApi('messages', { method: 'POST', body: { customer_id: pick(), channel: crmVal(body, 'mn-ch'), purpose: crmVal(body, 'mn-pu'), template_id: crmVal(body, 'mn-t'), text: body.querySelector('#mn-x').value.trim() } });
    if (r) body.querySelector('#mn-out').innerHTML = `<div class="crm-note">${crmStatus(r.status)} ${crmWhy(r)}</div>`;
  });
}

// --- settings ---------------------------------------------------------------------

async function crmSettings(host, st) {
  if (st.page === 'sender') return crmSenderEdit(host, st.params.id);
  const body = crmFrame(host, { title: 'Settings' });
  const [senders, life, ready] = await Promise.all([crmApi('senders', { quiet: true }), crmApi('lifecycle'), crmApi('readiness', { quiet: true })]);
  const store = (typeof currentUserLocation === 'string' && currentUserLocation) || localStorage.getItem('erp_location') || '';
  body.innerHTML = `
    ${ready ? `<h3 class="crm-section">Ready to send</h3>${crmTable([{ label: 'Channel', key: 'channel' }, { label: 'Ready', html: r => r.ready ? crmChip('Yes', 'ok') : crmChip('Not yet', 'warn') },
      { label: 'Checks', html: r => (r.checks || []).map(c => `${c.ok ? '✓' : '✗'} ${crmEsc(c.name)}: ${crmEsc(c.detail)}`).join('<br>') }], ready, { empty: '' })}` : ''}
    <h3 class="crm-section">Senders</h3>
    ${senders ? crmTable([{ label: 'Channel', key: 'channel' }, { label: 'Provider', key: 'provider' }, { label: 'Name', html: s => `<a href="#" data-snd="${crmEsc(s.id)}">${crmEsc(s.name)}</a>` },
      { label: 'Status', html: s => s.status === 'Active' ? crmChip('Active', 'ok') : crmChip('Off', 'muted') },
      { label: 'Complete', html: s => s.problems && s.problems.length ? `${crmChip('Not yet', 'warn')}<small class="crm-reason">${crmEsc(s.problems.join('; '))}</small>` : crmChip('Yes', 'ok') }],
      senders.senders, { empty: 'No senders yet. Without one, messages are recorded as NO_SENDER and nothing goes out.' }) + '<button class="btn btn-primary btn-sm" type="button" id="snd-new">+ New sender</button>'
      : '<p class="text-muted">Only an administrator can see and change senders.</p>'}
    <h3 class="crm-section">Contact rules, quiet hours, time zone, test list</h3>
    <p>These are in Setup &gt; Configuration, under <b>CRM</b>: messages per customer per day and week, hours between offers, quiet hours, the customer time zone, the test numbers and emails, costs per message, the privacy-notice version and who sees full phone numbers. <button class="btn btn-outline btn-sm" type="button" id="crm-cfg">Open Configuration</button></p>
    <h3 class="crm-section">Lifecycle</h3>
    ${life ? `<form class="crm-form crm-form-inline" id="lc-form">${crmField('lc-new', 'New for (days)', crmInput('lc-new', life.thresholds.new_days, 'inputmode="numeric"'))}
      ${crmField('lc-act', 'Active within (days)', crmInput('lc-act', life.thresholds.active_days, 'inputmode="numeric"'))}
      ${crmField('lc-risk', 'At risk until (days), then lapsed', crmInput('lc-risk', life.thresholds.at_risk_days, 'inputmode="numeric"'))}
      <button class="btn btn-outline" type="submit">Save as a new version</button></form>
      <p class="text-muted crm-small">In use: version ${life.thresholds.version || 'default'}. ${(life.counts || []).map(c => `${crmEsc(c.state)} ${c.count}`).join(' · ')}</p>` : ''}
    <h3 class="crm-section">Appointment hours at a store</h3>
    <form class="crm-form crm-form-inline" id="sl-form">${crmField('sl-store', 'Store code', crmInput('sl-store', store))}<button class="btn btn-outline btn-sm" type="button" id="sl-load">Open</button></form><div id="sl-out"></div>
    <h3 class="crm-section">Who approves</h3>
    <p>Campaigns, customer merges and sales quotations go through Approvals (Setup &gt; Approval Rules). The usual rule - a Store Manager approves - is added for you; a campaign can be made to always need a second person (Configuration &gt; CRM).</p>`;
  body.querySelectorAll('[data-snd]').forEach(a => a.addEventListener('click', (e) => { e.preventDefault(); crmGo('settings', 'sender', { id: a.dataset.snd }); }));
  body.querySelector('#snd-new')?.addEventListener('click', () => crmGo('settings', 'sender', {}));
  body.querySelector('#crm-cfg').addEventListener('click', () => renderView('configuration'));
  body.querySelector('#lc-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const r = await crmApi('lifecycle', { method: 'PUT', body: { new_days: Number(crmVal(body, 'lc-new')), active_days: Number(crmVal(body, 'lc-act')), at_risk_days: Number(crmVal(body, 'lc-risk')) } });
    if (r) { showToast(`Saved - version ${r.version}.`, { variant: 'success' }); crmSettings(host, st); }
  });
  body.querySelector('#sl-load').addEventListener('click', async () => {
    const code = crmVal(body, 'sl-store');
    const s = await crmApi(`stores/${encodeURIComponent(code)}/slots`);
    if (!s) return;
    const tpls = ((await crmApi('templates')) || {}).templates || [];
    const out = body.querySelector('#sl-out');
    out.innerHTML = `<form class="crm-form" id="sl-edit"><div class="crm-form-row">
      ${crmField('sl-o', 'Opens', crmInput('sl-o', s.opens, 'type="time"'))}${crmField('sl-c', 'Closes', crmInput('sl-c', s.closes, 'type="time"'))}
      ${crmField('sl-m', 'Slot (minutes)', crmInput('sl-m', s.slot_minutes, 'inputmode="numeric"'))}${crmField('sl-cap', 'Bookings per slot', crmInput('sl-cap', s.capacity, 'inputmode="numeric"'))}
      ${crmField('sl-d', 'Days (1 = Mon ... 7 = Sun)', crmInput('sl-d', s.days))}</div><div class="crm-form-row">
      ${crmField('sl-rc', 'Reminders by', crmSelect('sl-rc', CH, s.reminder_channel))}
      ${crmField('sl-rt', 'Reminder template', crmSelect('sl-rt', [['', 'A plain reminder (WhatsApp in the reply window, or email)']].concat(tpls.filter(t => t.purpose === 'service').map(t => [t.id, `${t.name} (${t.channel})`])), s.reminder_template_id || ''))}</div>
      <button class="btn btn-primary btn-sm" type="submit">Save</button></form>`;
    out.querySelector('#sl-edit').addEventListener('submit', async (e) => {
      e.preventDefault();
      const r = await crmApi(`stores/${encodeURIComponent(code)}/slots`, { method: 'PUT', body: { opens: crmVal(out, 'sl-o'), closes: crmVal(out, 'sl-c'), slot_minutes: Number(crmVal(out, 'sl-m')),
        capacity: Number(crmVal(out, 'sl-cap')), days: crmVal(out, 'sl-d'), reminder_channel: crmVal(out, 'sl-rc'), reminder_template_id: crmVal(out, 'sl-rt') } });
      if (r) showToast('Saved.', { variant: 'success' });
    });
  });
}

const crmProviders = {
  WhatsApp: [['whatsapp_cloud', 'WhatsApp Business Cloud API'], ['webhook', 'Your own gateway (webhook)'], ['mock', 'Test only - nothing leaves']],
  SMS: [['sms_http', 'DLT SMS gateway (HTTP)'], ['webhook', 'Your own gateway (webhook)'], ['mock', 'Test only - nothing leaves']],
  Email: [['email_smtp', 'Mail account (SMTP)'], ['webhook', 'Your own gateway (webhook)'], ['mock', 'Test only - nothing leaves']],
};
const crmProviderFields = {
  email_smtp: { config: [['host', 'Mail server (blank = this server\'s)'], ['port', 'Port'], ['username', 'User name'], ['from_email', 'From address'], ['from_name', 'From name'], ['reply_to', 'Reply-to']], secret: [['password', 'Password']] },
  sms_http: { config: [['url', 'Send URL (https)'], ['method', 'Method (POST / GET)'], ['format', 'Body (json / form)'], ['to_param', 'Number parameter'], ['to_format', 'Number as (e164 / digits / national)'],
    ['message_param', 'Text parameter'], ['sender_param', 'Header parameter'], ['sender_value', 'DLT sender header (e.g. SEZNIK)'], ['entity_param', 'Entity ID parameter'], ['entity_value', 'DLT principal entity ID'],
    ['template_param', 'DLT template ID parameter'], ['ref_param', 'Our reference parameter (optional)'], ['auth_header', 'Auth header name'], ['auth_param', 'Or auth parameter name'], ['extra', 'Fixed parameters (JSON)'], ['test_template_id', 'DLT template ID for a test'], ['id_field', 'Message id in the answer (e.g. data.id)'],
    ['dlr_id_param', 'Report: message id parameter'], ['dlr_status_param', 'Report: status parameter'], ['dlr_cost_param', 'Report: cost parameter']], secret: [['auth_value', 'Auth header value (API key)']] },
  whatsapp_cloud: { config: [['phone_number_id', 'Phone number ID'], ['waba_id', 'WhatsApp business account ID'], ['api_version', 'API version (v21.0)'], ['verify_token', 'Webhook verify token']], secret: [['access_token', 'Access token'], ['app_secret', 'App secret']] },
  webhook: { config: [['url', 'Webhook URL (https)'], ['id_field', 'Message id in the answer']], secret: [['signing_secret', 'Signing secret']] },
  mock: { config: [['mode', 'Behaviour (accept / deliver / reject / timeout)']], secret: [] },
};

async function crmSenderEdit(host, id) {
  let s = { channel: 'WhatsApp', provider: 'whatsapp_cloud', status: 'Active', config: {}, secrets_set: {} };
  if (id) { const list = await crmApi('senders'); s = ((list || {}).senders || []).find(x => x.id === id) || s; }
  const body = crmFrame(host, { title: id ? s.name : 'New sender', back: () => crmGo('settings'), backLabel: 'Settings' });
  const paint = () => {
    const f = crmProviderFields[s.provider] || { config: [], secret: [] };
    body.innerHTML = `<form class="crm-form" id="sd-form"><div class="crm-form-row">
      ${crmField('sd-ch', 'Channel', crmSelect('sd-ch', CH, s.channel, id ? 'disabled' : ''))}
      ${crmField('sd-pr', 'Provider', crmSelect('sd-pr', crmProviders[s.channel] || [], s.provider, id ? 'disabled' : ''))}
      ${crmField('sd-name', 'Name', crmInput('sd-name', s.name || ''))}${crmField('sd-st', 'Status', crmSelect('sd-st', [['Active', 'Active'], ['Inactive', 'Off']], s.status))}</div>
      <div class="crm-form-grid">${f.config.map(([k, l]) => crmField('sdc-' + k, l, crmInput('sdc-' + k, (s.config || {})[k] || ''))).join('')}</div>
      <div class="crm-form-grid">${f.secret.map(([k, l]) => crmField('sds-' + k, l + ((s.secrets_set || {})[k] ? ' (set - blank keeps it)' : ''), `<input class="form-input" type="password" id="sds-${k}" autocomplete="new-password">`)).join('')}</div>
      ${s.callback_url ? `<p class="crm-small"><b>Callback URL</b> (give this to the provider for delivery reports${s.provider === 'whatsapp_cloud' ? ' and customer replies' : ''}): <code>${crmEsc(s.callback_url.startsWith('/') ? location.origin + s.callback_url : s.callback_url)}</code></p>` : ''}
      ${(s.problems || []).length ? `<div class="crm-note crm-note-warn">${s.problems.map(crmEsc).join('<br>')}</div>` : ''}
      <div class="crm-form-actions"><button class="btn btn-primary" type="submit">Save</button>${s.provider === 'sms_http' ? ' <select class="form-select" id="sd-preset" style="width:auto;"><option value="">Fill from...</option></select>' : ''}${id ? ' <button class="btn btn-outline" type="button" id="sd-test">Test connection</button>' : ''}</div></form>
      <p class="text-muted crm-small">Each channel uses its one Active sender; saving another as Active switches the old one off. Secrets are stored encrypted and never shown again. A server that is not production sends only to the test list (Configuration &gt; CRM).</p>`;
    body.querySelector('#sd-ch').addEventListener('change', () => { s.channel = crmVal(body, 'sd-ch'); s.provider = crmProviders[s.channel][0][0]; paint(); });
    body.querySelector('#sd-pr').addEventListener('change', () => { s.provider = crmVal(body, 'sd-pr'); paint(); });
    // 140.12.26: provider presets fill the gateway's parameter names; Test connection asks the provider.
    const presetSel = body.querySelector('#sd-preset');
    if (presetSel) crmApi('sender-presets', { quiet: true }).then(list => {
      (list || []).forEach(p => presetSel.insertAdjacentHTML('beforeend', `<option value="${crmEsc(p.key)}">${crmEsc(p.label)}</option>`));
      presetSel.addEventListener('change', () => {
        const p = (list || []).find(x => x.key === presetSel.value);
        if (!p) return;
        Object.entries(p.config || {}).forEach(([k, v]) => { const el = body.querySelector('#sdc-' + k); if (el) el.value = typeof v === 'object' ? JSON.stringify(v) : v; });
        if (!crmVal(body, 'sd-name')) body.querySelector('#sd-name').value = p.label;
        showToast(`${p.label}: fill the key (${p.secret_hint}) and your DLT details, then save. ${p.note}`);
      });
    });
    body.querySelector('#sd-test')?.addEventListener('click', async () => {
      let to = '';
      if (s.channel === 'SMS') { to = String((await showCustomPrompt('Send one test SMS to (a number on the test list):', '', 'Test connection')) || '').trim(); if (!to) return; }
      const r = await crmApi('senders/' + encodeURIComponent(id) + '/test', { method: 'POST', body: { to } });
      if (r) await showCustomAlert(`${r.ok ? 'Works' : 'Not working'}: ${r.detail}`, 'Test connection');
    });
    body.querySelector('#sd-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const config = {}, secrets = {};
      f.config.forEach(([k]) => { const v = crmVal(body, 'sdc-' + k); if (v) config[k] = v; });
      f.secret.forEach(([k]) => { const v = body.querySelector('#sds-' + k).value; if (v) secrets[k] = v; });
      const r = await crmApi(id ? 'senders/' + encodeURIComponent(id) : 'senders', { method: id ? 'PUT' : 'POST', body: { channel: s.channel, provider: s.provider, name: crmVal(body, 'sd-name'), status: crmVal(body, 'sd-st'), config, secrets } });
      if (r) { showToast('Saved.', { variant: 'success' }); crmGo('settings', 'sender', { id: r.id }); }
    });
  };
  paint();
}
