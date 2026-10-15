// Shared lazy-view link for a saved document. The generic record list's
// server-side search accepts the stored id, so a link remains useful even when
// the specialized workbench has no record-detail route of its own.
const ESCAPE = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
function safeText(value) {
  return String(value ?? '').replace(/[&<>"']/g, char => ESCAPE[char]);
}

function documentSearchLink(doctype, id, label = id) {
  if (!id) return '<span class="text-muted">—</span>';
  const href = `#/setup/${encodeURIComponent(doctype)}?q=${encodeURIComponent(String(id))}`;
  return `<a href="${safeText(href)}" title="Find ${safeText(doctype)} ${safeText(id)}">${safeText(label)}</a>`;
}

export { documentSearchLink };
