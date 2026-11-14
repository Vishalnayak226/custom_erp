// Category attribute rules (168.12 / 168.8). Lazy module for the PIM "Category
// Rules" page, plus the item panel's rule wiring (applyItemRules): list first,
// the New rule form on its own page. A rule says Required, Optional or Hidden
// for one attribute in one category; a sub-category inherits its parent's rules
// and its own rule wins.

const RULE_WORDS = { Required: 'Required', Optional: 'Optional', Hidden: 'Hidden' };

async function loadList(doctype) {
  const res = await apiFetch(`/api/v1/doc/${doctype}`);
  return res && res.ok ? await res.json() : [];
}

const idOf = r => r.code || r.id;
const catName = c => c.name || idOf(c);

export async function renderCategoryRules(container) {
  const [cats, attrs, rules] = await Promise.all([loadList('Category'), loadList('ProductAttributeDef'), loadList('CategoryAttributeRule')]);
  const { txnPagedSection, txnRestorePane, txnDone } = await import('./txn-screen.js' + new URL(import.meta.url).search);
  const root = document.createElement('div');
  root.id = 'pim-category-rules';
  container.appendChild(root);

  const catById = Object.fromEntries(cats.map(c => [idOf(c), c]));
  const attrLabel = id => { const a = attrs.find(x => idOf(x) === id); return a ? (a.label || id) : id; };
  const selected = (() => { try { return sessionStorage.getItem('pim-cat-rules-cat') || ''; } catch (e) { return ''; } })();

  const form = document.createElement('div');
  form.className = 'table-panel';
  form.innerHTML = `
    <div class="form-group"><label class="form-label" for="pcr-cat">Category</label>
      <select id="pcr-cat" class="form-input">${cats.map(c => `<option value="${escapeHTMLText(idOf(c))}"${idOf(c) === selected ? ' selected' : ''}>${escapeHTMLText(catName(c))}</option>`).join('')}</select></div>
    <div class="form-group"><label class="form-label" for="pcr-attr">Attribute</label>
      <select id="pcr-attr" class="form-input">${attrs.map(a => `<option value="${escapeHTMLText(idOf(a))}">${escapeHTMLText(a.label || idOf(a))}</option>`).join('')}</select></div>
    <div class="form-group"><label class="form-label" for="pcr-rule">Rule</label>
      <select id="pcr-rule" class="form-input">${Object.keys(RULE_WORDS).map(r => `<option>${r}</option>`).join('')}</select></div>
    <div class="form-group"><label class="form-label" for="pcr-default">Default value</label>
      <input id="pcr-default" class="form-input"></div>
    <div id="pcr-err" class="login-error hidden"></div>
    <button class="btn btn-primary" id="pcr-save">Save</button>`;

  const list = document.createElement('div');
  list.className = 'table-panel';
  list.innerHTML = `
    <div class="form-group"><label class="form-label" for="pcr-pick">Category</label>
      <select id="pcr-pick" class="form-input">${cats.length === 0 ? '<option value="">No categories</option>' : cats.map(c => `<option value="${escapeHTMLText(idOf(c))}"${idOf(c) === selected ? ' selected' : ''}>${escapeHTMLText(catName(c))}</option>`).join('')}</select></div>
    <div class="table-wrapper"><table>
      <thead><tr><th>Attribute</th><th>Rule</th><th>Default</th><th>Source</th><th>Actions</th></tr></thead>
      <tbody id="pcr-body"><tr><td colspan="5" class="text-muted">Loading…</td></tr></tbody>
    </table></div>`;

  root.appendChild(txnPagedSection({ id: 'pim-cat-rules', title: 'Category rules', page: 'New rule', count: rules.length, newLabel: 'New rule', form, list }));
  txnRestorePane('pim-cat-rules');

  const body = root.querySelector('#pcr-body');
  const ownRule = (cat, attr) => rules.find(r => r.category === cat && r.attribute === attr);
  const refresh = async () => {
    const cat = root.querySelector('#pcr-pick').value;
    try { sessionStorage.setItem('pim-cat-rules-cat', cat); } catch (e) { /* optional */ }
    if (!cat) { body.innerHTML = '<tr><td colspan="5" class="text-muted">None yet</td></tr>'; return; }
    const res = await apiFetch(`/api/v1/pim/category-rules?category=${encodeURIComponent(cat)}`);
    if (!res || !res.ok) { body.innerHTML = '<tr><td colspan="5" class="text-muted">Could not load</td></tr>'; return; }
    const out = (await res.json()).rules || [];
    body.innerHTML = out.length === 0
      ? '<tr><td colspan="5" class="text-muted">None yet</td></tr>'
      : out.map(r => {
        const own = !r.inherited ? ownRule(cat, r.attribute) : null;
        const from = r.inherited ? `from ${escapeHTMLText(catName(catById[r.from] || { name: r.from }))}` : 'This category';
        return `<tr><td>${escapeHTMLText(r.label || attrLabel(r.attribute))}</td><td><span class="badge badge-secondary">${escapeHTMLText(r.rule)}</span></td>
          <td>${escapeHTMLText(r.default_value || '')}</td><td>${from}</td>
          <td>${own ? `<button class="action-btn action-btn-danger" data-pcr-remove="${escapeHTMLText(idOf(own))}">Remove</button>` : ''}</td></tr>`;
      }).join('');
  };
  root.querySelector('#pcr-pick').addEventListener('change', refresh);
  body.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-pcr-remove]');
    if (!b) return;
    if (!(await showCustomConfirm('Remove this rule?'))) return;
    const res = await apiFetch(`/api/v1/doc/CategoryAttributeRule/${encodeURIComponent(b.dataset.pcrRemove)}`, { method: 'DELETE' });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Not removed.'); return; }
    renderView('pim:category-rules');
  });
  root.querySelector('#pcr-save').addEventListener('click', async (e) => {
    const btn = e.currentTarget;
    const err = root.querySelector('#pcr-err');
    err.classList.add('hidden');
    const category = root.querySelector('#pcr-cat').value;
    const attribute = root.querySelector('#pcr-attr').value;
    if (!category || !attribute) { err.textContent = 'Pick a category and an attribute.'; err.classList.remove('hidden'); return; }
    const id = `CAR-${category}-${attribute}`;
    await guardAgainstDoubleSubmit(btn, 'Saving…', async () => {
      const res = await apiFetch('/api/v1/doc/CategoryAttributeRule', { method: 'POST', body: JSON.stringify({
        id, code: id, category, attribute, rule: root.querySelector('#pcr-rule').value,
        default_value: root.querySelector('#pcr-default').value.trim(), status: 'Active' }) });
      if (!res) return;
      if (!res.ok) { const d = await res.json().catch(() => ({})); err.textContent = d.error || 'Not saved.'; err.classList.remove('hidden'); return; }
      try { sessionStorage.setItem('pim-cat-rules-cat', category); } catch (x) { /* optional */ }
      txnDone('pim-cat-rules');
      renderView('pim:category-rules');
    });
  });
  await refresh();
}

// applyItemRules wires an item's attribute box to its category: Hidden
// attributes leave the list (their stored values stay), Required ones are
// marked, a default value pre-fills the box, and a Number's unit shows beside it.
export async function applyItemRules(panel, itemCode, attrDefs) {
  const select = panel.querySelector('#pim-attr-select');
  if (!select) return;
  const itemRes = await apiFetch(`/api/v1/doc/Item/${encodeURIComponent(itemCode)}`);
  const item = itemRes && itemRes.ok ? await itemRes.json() : null;
  const category = item && item.category;
  let byAttr = {};
  if (category) {
    const res = await apiFetch(`/api/v1/pim/category-rules?category=${encodeURIComponent(category)}`);
    if (res && res.ok) ((await res.json()).rules || []).forEach(r => { byAttr[r.attribute] = r; });
  }
  [...select.options].forEach(o => {
    const r = byAttr[o.value];
    if (r && r.rule === 'Hidden') { o.remove(); return; }
    if (r && r.rule === 'Required' && !o.textContent.endsWith(' *')) o.textContent += ' *';
  });
  const unitOf = code => { const a = attrDefs.find(x => idOf(x) === code); return a && a.value_type === 'Number' && a.uom ? a.uom : ''; };
  const showUnit = () => {
    const box = panel.querySelector('#pim-attr-value');
    let tag = panel.querySelector('#pim-attr-unit');
    if (!tag) { tag = document.createElement('span'); tag.id = 'pim-attr-unit'; tag.className = 'text-muted'; box.after(tag); }
    const u = unitOf(select.value);
    tag.textContent = u;
    const r = byAttr[select.value];
    if (r && r.default_value && !box.value && !box.multiple) box.value = r.default_value;
  };
  select.addEventListener('change', () => setTimeout(showUnit, 0));
}
