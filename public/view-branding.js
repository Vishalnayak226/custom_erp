// Stage 134.10 (120.6): Configuration -> Branding. Below the Accent colour
// setting (rendered by the settings page itself): the company logo - upload,
// preview, remove - and, for the platform operator, the switch that turns
// branding on for this client (the client_branding plan feature). The server
// decides every part again: PNG / JPEG / WebP only, at most 300 KB, an accent
// dark enough for white text, and only a Super Admin may switch the mode.

export async function renderBrandingPanel(host) {
  const panel = document.createElement('div');
  panel.className = 'brand-panel';
  panel.style.cssText = 'max-width: 640px; border-top: 1px solid var(--border-color); padding-top: 18px; margin-top: 6px;';
  host.appendChild(panel);
  const res = await apiFetch('/api/v1/branding');
  const b = res && res.ok ? await res.json() : { enabled: false };
  let role = '';
  try { role = String(localStorage.getItem('erp_role') || '').replace(/\s+/g, '').toLowerCase(); } catch (_) { /* storage off */ }
  const operator = role === 'superadmin';
  panel.innerHTML = `
    <div class="form-group s-2f23af">
      <label class="form-label s-21a1be">Branding for this company</label>
      <p class="page-subtitle s-19cee9">${b.enabled
        ? 'On.'
        : 'Off - ask Wholeops to switch it on.'}</p>
      ${operator ? `<label class="s-facf9d"><input type="checkbox" id="brand-mode" ${b.enabled ? 'checked' : ''} class="s-c98093"> <span class="s-a54188">Branding switched on for this client (platform operator)</span></label>` : ''}
    </div>
    <div class="form-group">
      <label class="form-label s-21a1be" for="brand-logo-file">Company logo</label>
      <p class="page-subtitle s-19cee9">PNG, JPEG or WebP (no SVG), at most 300 KB.</p>
      <div id="brand-logo-preview" class="s-39ae4d">${b.logo_version
        ? `<img src="/api/v1/branding/logo?v=${encodeURIComponent(b.logo_version)}" alt="Current logo" class="s-e69668">`
        : '<span class="text-muted s-a54188">No logo yet.</span>'}</div>
      <div class="s-aea80b">
        <input type="file" id="brand-logo-file" accept="image/png,image/jpeg,image/webp" class="form-input s-4e15a3">
        <button type="button" class="btn btn-primary btn-sm" id="brand-logo-upload">Upload logo</button>
        ${b.logo_version ? '<button type="button" class="btn btn-outline btn-sm" id="brand-logo-remove">Remove logo</button>' : ''}
      </div>
      <div id="brand-logo-error" class="login-error hidden s-e5a6d3" role="alert"></div>
      ${!b.enabled ? '<p class="text-muted s-f47227">You can upload it now; it shows once branding is on.</p>' : ''}
    </div>`;
  const showError = msg => {
    const el = panel.querySelector('#brand-logo-error');
    el.textContent = msg;
    el.classList.toggle('hidden', !msg);
  };
  const reload = () => { panel.remove(); renderBrandingPanel(host); };
  panel.querySelector('#brand-logo-upload').addEventListener('click', async () => {
    const file = panel.querySelector('#brand-logo-file').files[0];
    if (!file) { showError('Choose the logo file first.'); return; }
    const form = new FormData();
    form.append('file', file);
    const r = await apiUpload('/api/v1/admin/branding/logo', form);
    if (!r) return;
    if (!r.ok) { const e = await r.json().catch(() => ({})); showError(e.message || e.error || 'The logo was not saved.'); return; }
    window.showToast?.('Logo saved. It shows on the next page load.', { variant: 'success' });
    reload();
  });
  panel.querySelector('#brand-logo-remove')?.addEventListener('click', async () => {
    if (!(await window.showCustomConfirm('Remove the logo? The standard one shows again.', 'Remove logo'))) return;
    const r = await apiFetch('/api/v1/admin/branding/logo', { method: 'DELETE' });
    if (r && r.ok) reload(); else showError('The logo was not removed.');
  });
  panel.querySelector('#brand-mode')?.addEventListener('change', async (e) => {
    const r = await apiFetch('/api/v1/admin/tenant/feature-flag', { method: 'POST', body: JSON.stringify({ feature_name: 'client_branding', enabled: e.target.checked }) });
    if (!r || !r.ok) { e.target.checked = !e.target.checked; showError('Only the platform operator can switch branding.'); return; }
    window.showToast?.(`Branding ${e.target.checked ? 'on' : 'off'} for this client. It shows on the next page load.`, { variant: 'success' });
    reload();
  });
}

