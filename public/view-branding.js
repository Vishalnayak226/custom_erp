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
    <div class="form-group" style="margin-bottom: 18px;">
      <label class="form-label" style="font-weight: 600;">Branding for this company</label>
      <p class="page-subtitle" style="margin: 2px 0 8px; font-size: 12.5px;">${b.enabled
        ? 'On: your logo leads on your sign-in page, in the header and the menu, and your accent colour is used. Wholeops shows small beside it: "Powered by Wholeops" next to your logo in the menu, and a slim Wholeops strip across the top of the sign-in page.'
        : 'Off: the standard Wholeops look. Branding is part of the plan - ask Wholeops to switch it on.'}</p>
      ${operator ? `<label style="display: inline-flex; align-items: center; gap: 8px; cursor: pointer;"><input type="checkbox" id="brand-mode" ${b.enabled ? 'checked' : ''} style="width: 16px; height: 16px;"> <span style="font-size: 13px;">Branding switched on for this client (platform operator)</span></label>` : ''}
    </div>
    <div class="form-group">
      <label class="form-label" for="brand-logo-file" style="font-weight: 600;">Company logo</label>
      <p class="page-subtitle" style="margin: 2px 0 8px; font-size: 12.5px;">PNG, JPEG or WebP (no SVG), at most 300 KB. A wide logo on a transparent background looks best; it is shown at most 36 px high in the header and 64 px on the sign-in page.</p>
      <div id="brand-logo-preview" style="margin-bottom: 10px;">${b.logo_version
        ? `<img src="/api/v1/branding/logo?v=${encodeURIComponent(b.logo_version)}" alt="Current logo" style="max-height: 64px; max-width: 240px; padding: 8px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-card, #fff);">`
        : '<span class="text-muted" style="font-size: 13px;">No logo yet.</span>'}</div>
      <div style="display: flex; gap: 8px; flex-wrap: wrap;">
        <input type="file" id="brand-logo-file" accept="image/png,image/jpeg,image/webp" class="form-input" style="max-width: 320px;">
        <button type="button" class="btn btn-primary btn-sm" id="brand-logo-upload">Upload logo</button>
        ${b.logo_version ? '<button type="button" class="btn btn-outline btn-sm" id="brand-logo-remove">Remove logo</button>' : ''}
      </div>
      <div id="brand-logo-error" class="login-error hidden" role="alert" style="margin-top: 10px;"></div>
      ${!b.enabled ? '<p class="text-muted" style="font-size: 12.5px; margin-top: 8px;">You can upload it now; it shows once branding is on.</p>' : ''}
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

