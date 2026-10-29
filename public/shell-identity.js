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
.has-client-logo { gap: 12px; text-decoration: none; }
.client-logo { display: block; max-height: 36px; max-width: 160px; width: auto; height: auto; object-fit: contain; }
.sidebar-header .client-logo { max-height: 44px; max-width: 120px; }
.login-brand .client-logo { max-height: 64px; max-width: 240px; }
.wo-by { display: flex; align-items: center; gap: 12px; flex-shrink: 0; }
.wo-by::before { content: ''; width: 1px; height: 34px; background: var(--border-color); }
.wo-by > span { display: flex; flex-direction: column; gap: 3px; font-size: 10.5px; line-height: 1.2; color: var(--text-muted); white-space: nowrap; }
.wo-by img { height: 16px; width: auto; }
.wo-logo { display: var(--logo-light-display); }
.wo-logo-dark { display: var(--logo-dark-display); }
.wo-strip { position: absolute; top: 0; left: 0; right: 0; height: 44px; display: flex; align-items: center; padding: 0 24px; background: var(--panel-bg); border-bottom: 1px solid var(--border-color); }
.wo-strip img { height: 22px; width: auto; }
@media (max-width: 820px) { .header-brand .wo-by { display: none; } }
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

// Stage 141.4: the same request as the sidebar's version line (app.js).
(window.versionInfo ? window.versionInfo() : fetch('/api/v1/version', { cache: 'no-store' }).then(r => (r.ok ? r.json() : null)))
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
// of the Wholeops one on the sign-in card, the header and the menu, and its
// accent colour. Off, nothing changes.
//
// Stage 141.3: the last answer is kept in this browser (erp_brand, and the
// accent CSS in erp_brand_css). It is applied the moment this module loads,
// and theme-boot.js uses it before first paint (data-brand="client" keeps the
// Wholeops logo out of the client's logo slot; the accent is on from the
// start), so a refresh never flashes Wholeops, and a refused or failed
// branding call keeps the client's brand instead of dropping it.
let brand = null;
try { brand = JSON.parse(localStorage.getItem('erp_brand')); } catch (_) { /* blocked */ }

function remember(key, value) {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch (_) { /* blocked: this page still shows it */ }
}

// mix blends #rrggbb toward #rrggbb by t (0..1).
function mix(a, b, t) {
  const c = i => Math.round(parseInt(a.substr(1 + 2 * i, 2), 16) * (1 - t) + parseInt(b.substr(1 + 2 * i, 2), 16) * t);
  return '#' + [0, 1, 2].map(i => c(i).toString(16).padStart(2, '0')).join('');
}

// applyBranding brings the page to `brand` - on, off or changed - so it can
// run again whenever a newer answer arrives.
function applyBranding() {
  const on = !!(brand && brand.enabled);
  let css = '';
  if (on && /^#[0-9a-f]{6}$/i.test(brand.accent || '')) {
    const a = brand.accent;
    const light = `--primary-color: ${a}; --primary-hover: ${mix(a, '#000000', 0.15)}; --primary-light: ${mix(a, '#ffffff', 0.9)};`;
    const dark = `--primary-color: ${mix(a, '#ffffff', 0.35)}; --primary-hover: ${mix(a, '#ffffff', 0.5)}; --primary-light: ${mix(a, '#000000', 0.6)};`;
    css = `:root { ${light} }
:root[data-theme="dark"] { ${dark} }
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { ${dark} } }`;
  }
  let style = document.getElementById('brand-accent-css');
  if (css && !style) {
    style = document.createElement('style');
    style.id = 'brand-accent-css';
    document.head.appendChild(style);
  }
  if (style && !css) style.remove();
  else if (style && style.textContent !== css) style.textContent = css;
  remember('erp_brand_css', css);

  const src = on && brand.logo_version ? `/api/v1/branding/logo?v=${encodeURIComponent(brand.logo_version)}` : '';
  if (src) document.documentElement.setAttribute('data-brand', 'client');
  else document.documentElement.removeAttribute('data-brand');
  document.querySelectorAll('.login-brand, .app-header .header-brand, .sidebar-header .brand-link').forEach(host => {
    let img = host.querySelector('.client-logo');
    if (!src) {
      if (img) img.remove();
      host.classList.remove('has-client-logo');
      return;
    }
    if (!img) {
      img = document.createElement('img');
      img.className = 'client-logo';
      host.prepend(img);
    }
    if (img.getAttribute('src') !== src) img.src = src;
    img.alt = brand.company_name || 'Company logo';
    host.classList.add('has-client-logo');
  });
  showWholeops(!!src);
}

// Stage 141.1 (user's pick, 2026-10-29: "c1 b2"): beside a client's logo,
// Wholeops shows as its real logo, smaller - in the app (B) a divider and
// "Powered by" over the Wholeops logo, right of the client's logo in the menu
// head and beside the menu toggle while the menu is hidden (not on a phone);
// on the sign-in page (C) a slim Wholeops strip across the top. styles.css
// leaves room for the strip from first paint (data-brand), so nothing moves.
const WHOLEOPS = '<img class="wo-logo" src="/brand-logo.png" alt="Wholeops"><img class="wo-logo wo-logo-dark" src="/brand-logo-dark.png" alt="Wholeops">';

function showWholeops(on) {
  document.querySelectorAll('.app-header .header-brand, .sidebar-header .brand-link').forEach(host => {
    const by = host.querySelector('.wo-by');
    if (!on) {
      if (by) by.remove();
    } else if (!by) {
      host.insertAdjacentHTML('beforeend', `<span class="wo-by"><span>Powered by ${WHOLEOPS}</span></span>`);
    }
  });
  const screen = document.getElementById('login-screen');
  const strip = screen && screen.querySelector(':scope > .wo-strip');
  if (!on) {
    if (strip) strip.remove();
  } else if (screen && !strip) {
    screen.insertAdjacentHTML('afterbegin', `<div class="wo-strip">${WHOLEOPS}</div>`);
  }
}

applyBranding();
fetch('/api/v1/branding', { cache: 'no-store' })
  .then(r => (r.ok ? r.json() : null))
  .then(b => {
    if (!b) return; // refused or failed: keep the last brand
    brand = b;
    remember('erp_brand', JSON.stringify(b));
    applyBranding();
  })
  .catch(() => { /* offline: the last brand stays */ });

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
