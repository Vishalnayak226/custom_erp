(function () {
  function escapeHTML(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  var params = new URLSearchParams(location.search);
  var token = params.get('token') || '';
  var tenantID = params.get('tenant_id') || 'default';
  var titleEl = document.getElementById('share-title');
  var countEl = document.getElementById('share-count');
  var gridEl = document.getElementById('share-grid');
  var stateEl = document.getElementById('share-state');

  function showState(message) {
    gridEl.hidden = true;
    stateEl.hidden = false;
    stateEl.textContent = message;
  }

  if (!token) {
    titleEl.textContent = 'Shared Catalog';
    showState('This link is missing its share token.');
    return;
  }

  fetch('/api/v1/pim/catalog-share?token=' + encodeURIComponent(token) + '&tenant_id=' + encodeURIComponent(tenantID), {
    headers: { 'X-Tenant-ID': tenantID }
  })
    .then(function (res) {
      if (!res.ok) { throw new Error('not-found'); }
      return res.json();
    })
    .then(function (view) {
      titleEl.textContent = view.catalog_name || 'Shared Catalog';
      var products = view.products || [];
      if (products.length === 0) {
        showState('This catalog currently has no products.');
        return;
      }
      countEl.hidden = false;
      countEl.textContent = products.length + ' product' + (products.length === 1 ? '' : 's');
      gridEl.innerHTML = products.map(function (p) {
        return '<div class="share-card">' +
          '<div class="share-card-thumb">' + (p.has_main_image ? 'Image' : 'No image') + '</div>' +
          '<h3>' + escapeHTML(p.title || p.name || p.item_code) + '</h3>' +
          '<div class="code">' + escapeHTML(p.item_code) + '</div>' +
          (p.short_desc ? '<p class="desc">' + escapeHTML(p.short_desc) + '</p>' : '') +
          (p.category ? '<div class="category">' + escapeHTML(p.category) + '</div>' : '') +
          '</div>';
      }).join('');
    })
    .catch(function () {
      titleEl.textContent = 'Shared Catalog';
      showState('This share link is invalid, expired, or has been revoked.');
    });
})();
