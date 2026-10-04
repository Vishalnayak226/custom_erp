// Stage 28.2/47.8: apply the saved theme before first paint to avoid a
// light-mode flash. 'system' (or unset) lets the CSS media query follow the
// OS; 'light'/'dark' force a choice. app.js reconciles this with the
// server-stored per-user preference once /api/v1/me loads.
(function () {
  try {
    document.documentElement.setAttribute('data-theme', localStorage.getItem('erp-theme') || 'system');
  } catch (e) {}
})();
