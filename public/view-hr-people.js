// Ask 170 group E: the HR "People" tab - analytics, certificates that expire,
// accepting an offer and making a login from a finished onboarding checklist.
// Positions, openings, applicants, offers, disciplinary cases and holiday
// calendars are ordinary doctype lists and forms.
const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

async function getJSON(url) {
  const res = await apiFetch(url);
  if (!res || !res.ok) { if (res) await showApiError(res, 'Failed to load.', 'Load Failed'); return []; }
  return (await res.json()) || []; // an empty list comes back as null (200.2: People threw on a tenant with no employees)
}

async function post(url, body, out) {
  const res = await apiFetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
  if (!res || !res.ok) { if (res) await showApiError(res, 'Action failed.', 'Action Failed'); return; }
  const data = await res.json();
  out.textContent = data.employee_id ? `Done. Employee ${data.employee_id} created (Inactive).`
    : data.request_id ? 'Login made. It waits for approval.' : data.username ? `Login ${data.username} made. They set a password with Forgot password.` : 'Done.';
}

async function renderHRPeopleTab(container) {
  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '24px';
  panel.innerHTML = `
    <h3>People analytics</h3>
    <div class="form-group hr-w220"><label class="form-label" for="hrp-group">Group by</label>
      <select id="hrp-group" class="form-input"><option value="department">Department</option><option value="location">Location</option><option value="position">Position</option></select></div>
    <table><thead><tr><th>Group</th><th>Headcount</th><th>Joiners</th><th>Leavers</th><th>Attrition %</th></tr></thead><tbody id="hrp-analytics"></tbody></table>
    <h3 class="hr-mt24">Certificates expiring</h3>
    <div class="form-group hr-w220"><label class="form-label" for="hrp-days">Next days</label>
      <input id="hrp-days" class="form-input" type="number" value="30"></div>
    <table><thead><tr><th>Employee</th><th>Program</th><th>Certificate</th><th>Valid until</th><th>Days left</th></tr></thead><tbody id="hrp-certs"></tbody></table>
    <h3 class="hr-mt24">Accept offer</h3>
    <div class="form-group hr-w320"><label class="form-label" for="hrp-offer">Offer id</label><input id="hrp-offer" class="form-input"></div>
    <button class="btn btn-primary" id="hrp-accept">Accept</button>
    <h3 class="hr-mt24">Login from onboarding</h3>
    <div class="form-group hr-w320"><label class="form-label" for="hrp-onb">Checklist id</label><input id="hrp-onb" class="form-input"></div>
    <div class="form-group hr-w320"><label class="form-label" for="hrp-role">Role</label><input id="hrp-role" class="form-input"></div>
    <button class="btn btn-primary" id="hrp-login">Create login</button>
    <div id="hrp-msg" class="hr-muted hr-mt12"></div>`;
  container.appendChild(panel);
  const $ = id => panel.querySelector('#' + id);

  const loadAnalytics = async () => {
    const rows = await getJSON('/api/v1/hr/people-analytics?group_by=' + encodeURIComponent($('hrp-group').value));
    $('hrp-analytics').innerHTML = rows.length ? rows.map(r => `<tr><td>${esc(r.group)}</td><td>${r.headcount}</td><td>${r.joiners}</td><td>${r.leavers}</td><td>${r.attrition_pct}</td></tr>`).join('')
      : '<tr><td colspan="5">No employees.</td></tr>';
  };
  const loadCerts = async () => {
    const rows = await getJSON('/api/v1/hr/certificates-expiring?days=' + encodeURIComponent($('hrp-days').value || 30));
    $('hrp-certs').innerHTML = rows.length ? rows.map(r => `<tr><td>${esc(r.employee_id)}</td><td>${esc(r.program_id)}</td><td>${esc(r.certificate_no)}</td><td>${esc(r.valid_until)}</td><td>${r.days_left}</td></tr>`).join('')
      : '<tr><td colspan="5">Nothing expires in this time.</td></tr>';
  };
  $('hrp-group').addEventListener('change', loadAnalytics);
  $('hrp-days').addEventListener('change', loadCerts);
  $('hrp-accept').addEventListener('click', () => post('/api/v1/hr/job-offers/' + encodeURIComponent($('hrp-offer').value.trim()) + '/accept', {}, $('hrp-msg')));
  $('hrp-login').addEventListener('click', () => post('/api/v1/hr/onboarding/' + encodeURIComponent($('hrp-onb').value.trim()) + '/create-login', { role: $('hrp-role').value.trim() }, $('hrp-msg')));
  await Promise.all([loadAnalytics(), loadCerts()]);
}

export { renderHRPeopleTab };
