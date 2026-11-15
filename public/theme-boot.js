// Stage 28.2/47.8: apply the saved theme before first paint to avoid a
// light-mode flash. 'system' (or unset) lets the CSS media query follow the
// OS; 'light'/'dark' force a choice. app.js reconciles this with the
// server-stored per-user preference once /api/v1/me loads.
(function () {
  try {
    var root = document.documentElement;
    root.setAttribute('data-theme', localStorage.getItem('erp-theme') || 'system');
    // Stage 141.3: the last brand this browser saw (shell-identity.js keeps
    // it): no Wholeops logo in a client's logo slot, room for the sign-in
    // page's Wholeops strip, and its accent colour, from the first paint.
    var b = JSON.parse(localStorage.getItem('erp_brand') || 'null');
    if (b && b.enabled && b.logo_version) root.setAttribute('data-brand', 'client');
    var css = localStorage.getItem('erp_brand_css');
    if (css) {
      // 49.4.3: a constructed sheet, not a <style> element, so a strict
      // style-src allows it; app.js adoptCss finds it again by erpId.
      var s = new CSSStyleSheet();
      s.erpId = 'brand-accent-css';
      s.replaceSync(css);
      document.adoptedStyleSheets = Array.from(document.adoptedStyleSheets).concat(s);
    }
  } catch (e) {}
})();
