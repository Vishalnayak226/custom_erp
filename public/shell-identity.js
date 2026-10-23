// Stage 130.7 (120.5, user decision 2026-10-22): which environment and whose
// ERP this is, wherever a person looks. Off production the sign-in card shows
// a badge and the tab title starts with [Dev] (from the public /api/v1/version);
// the client's company name (Settings > Configuration > Platform > Company name
// in the header) sits beside the logo and in the tab title. Lazy, out of the
// cold core: app.js loads it at sign-in and once the profile arrives.

let envTag = '';
let company = '';

const TAGS = { development: 'Dev', staging: 'Staging', test: 'Test', uat: 'UAT' };

if (!document.getElementById('shell-identity-css')) {
  const style = document.createElement('style');
  style.id = 'shell-identity-css';
  style.textContent = `
.shell-env-badge { margin: 0 auto 12px; width: fit-content; padding: 2px 10px; border-radius: 999px; font-size: 12px; font-weight: 700; background: var(--warning-soft-bg); color: var(--warning-strong); }
.header-company { margin: 0 16px 0 4px; min-width: 0; max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; color: var(--text-muted); }
@media (max-width: 700px) { .header-company { display: none; } }
.has-client-logo .brand-logo, .has-client-logo .brand-symbol { display: none !important; }
.client-logo { display: block; max-height: 36px; max-width: 160px; width: auto; height: auto; object-fit: contain; }
.login-brand .client-logo { max-height: 64px; max-width: 240px; margin: 0 auto; }
.powered-by { margin: 12px 0 0; font-size: 11px; color: var(--text-muted); text-align: center; }
`;
  document.head.appendChild(style);
}

// fixTitle adds the environment tag and the company to whatever title the
// app just set ("Purchase Orders | Wholeops" -> "[Dev] Purchase Orders -
// Minn Jewels | Wholeops"). Setting it again re-runs the observer, which then
// finds nothing to change.
function fixTitle() {
  const base = document.title.replace(/^\[[^\]]+\] /, '');
  let t = base;
  if (company && !base.startsWith('Sign in') && !base.includes(company)) {
    t = base === 'Wholeops' ? `${company} | Wholeops` : base.replace(/ \| Wholeops$/, () => ` - ${company} | Wholeops`);
  }
  if (envTag) t = `[${envTag}] ${t}`;
  if (t !== document.title) document.title = t;
}

const titleEl = document.querySelector('title');
if (titleEl) new MutationObserver(fixTitle).observe(titleEl, { childList: true, characterData: true, subtree: true });

fetch('/api/v1/version', { cache: 'no-store' })
  .then(r => (r.ok ? r.json() : null))
  .then(v => {
    if (!v || !v.env) return;
    envTag = TAGS[v.env] || v.env;
    const card = document.querySelector('#login-screen .login-card');
    if (card && !card.querySelector('.shell-env-badge')) {
      const badge = document.createElement('div');
      badge.className = 'shell-env-badge';
      badge.textContent = `${v.env.charAt(0).toUpperCase()}${v.env.slice(1)} environment - not the live system`;
      card.prepend(badge);
    }
    fixTitle();
  })
  .catch(() => { /* offline: no badge */ });

// Stage 134.10 (120.6): a client's own brand, when the platform operator has
// switched branding on for it (public GET /api/v1/branding): its logo in place
// of the Wholeops one on the sign-in card, the header and the menu, its accent
// colour, and "Powered by Wholeops" kept at the foot. Off, nothing changes.
let brand = null;

// mix blends #rrggbb toward #rrggbb by t (0..1).
function mix(a, b, t) {
  const c = i => Math.round(parseInt(a.substr(1 + 2 * i, 2), 16) * (1 - t) + parseInt(b.substr(1 + 2 * i, 2), 16) * t);
  return '#' + [0, 1, 2].map(i => c(i).toString(16).padStart(2, '0')).join('');
}

function applyBranding() {
  if (!brand || !brand.enabled) return;
  if (/^#[0-9a-f]{6}$/i.test(brand.accent || '') && !document.getElementById('brand-accent-css')) {
    const a = brand.accent;
    const light = `--primary-color: ${a}; --primary-hover: ${mix(a, '#000000', 0.15)}; --primary-light: ${mix(a, '#ffffff', 0.9)};`;
    const dark = `--primary-color: ${mix(a, '#ffffff', 0.35)}; --primary-hover: ${mix(a, '#ffffff', 0.5)}; --primary-light: ${mix(a, '#000000', 0.6)};`;
    const style = document.createElement('style');
    style.id = 'brand-accent-css';
    style.textContent = `:root { ${light} }
:root[data-theme="dark"] { ${dark} }
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { ${dark} } }`;
    document.head.appendChild(style);
  }
  if (brand.logo_version) {
    const src = `/api/v1/branding/logo?v=${encodeURIComponent(brand.logo_version)}`;
    document.querySelectorAll('.login-brand, .app-header .header-brand, .sidebar-header .brand-link').forEach(host => {
      if (host.querySelector('.client-logo')) return;
      const img = document.createElement('img');
      img.className = 'client-logo';
      img.src = src;
      img.alt = brand.company_name || 'Company logo';
      host.classList.add('has-client-logo');
      host.prepend(img);
    });
  }
  document.querySelectorAll('.login-card, .sidebar-footer').forEach(host => {
    if (host.querySelector(':scope > .powered-by')) return;
    const p = document.createElement('p');
    p.className = 'powered-by';
    p.textContent = 'Powered by Wholeops';
    host.appendChild(p);
  });
}

fetch('/api/v1/branding', { cache: 'no-store' })
  .then(r => (r.ok ? r.json() : null))
  .then(b => { brand = b; applyBranding(); })
  .catch(() => { /* offline: the standard look */ });

// applyCompanyName shows the company beside the header logo and in the tab
// title; a blank name removes it.
export function applyCompanyName(name) {
  applyBranding();
  company = String(name || '').trim();
  const brand = document.querySelector('.app-header .header-brand');
  let el = document.getElementById('header-company');
  if (company && brand) {
    if (!el) {
      el = document.createElement('span');
      el.id = 'header-company';
      el.className = 'header-company';
      brand.after(el);
    }
    el.textContent = company;
    el.title = company;
  } else if (el) {
    el.remove();
  }
  fixTitle();
}
