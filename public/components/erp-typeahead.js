// <erp-typeahead> (21.14): the first of this codebase's Web Components.
//
// Why this exists: public/app.js's attachTypeahead() (still there, still
// used by ~15 call sites - not ripped out) proved the pattern works, but as
// a plain function bolted onto a plain <input> it can't encapsulate its own
// menu DOM/listeners - every call site's markup and the component's
// internals share one global namespace. Native Web Components give real
// encapsulation (Shadow DOM) with zero framework/build step, which is the
// whole point: this file is loaded with a plain <script> tag, same as
// app.js itself.
//
// Styling note: Shadow DOM isolates CSS *rules* (styles.css's
// .typeahead-menu/.typeahead-item selectors don't reach inside here), but
// CSS custom properties (the --panel-bg/--border-color/... design tokens)
// DO inherit through the shadow boundary - so this component ships its own
// minimal stylesheet (adopted via adoptedStyleSheets, still just CSS, no
// preprocessor) built entirely from those same tokens, staying visually
// identical to the rest of the app without duplicating token values.
//
// Usage: <erp-typeahead doctype="Vendor" name="vendor" placeholder="Search vendor...">
// Reads/writes its value via the `value` property (like a normal input) and
// dispatches a bubbling, composed 'change' event on pick, so existing
// `el.addEventListener('change', ...)` call sites work unchanged whether
// they're wired to a plain <input> (attachTypeahead) or this element.

const sheet = new CSSStyleSheet();
sheet.replaceSync(`
  :host { display: inline-block; width: 100%; }
  input {
    width: 100%;
    box-sizing: border-box;
    font: inherit;
    color: var(--text-main);
    background: var(--input-bg, var(--panel-bg));
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 8px 10px;
  }
  input:focus { outline: none; border-color: var(--primary-color); }
  .menu {
    position: fixed;
    z-index: 300;
    background-color: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    box-shadow: var(--shadow-lg);
    max-height: 220px;
    overflow-y: auto;
  }
  .item {
    padding: 8px 12px;
    font-size: 13px;
    color: var(--text-main);
    cursor: pointer;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item.active, .item:hover {
    background-color: var(--primary-color);
    color: #fff;
  }
`);

class ErpTypeahead extends HTMLElement {
  static get observedAttributes() { return ['value', 'placeholder', 'disabled']; }

  constructor() {
    super();
    this._items = [];
    this._activeIndex = -1;
    this._debounceTimer = null;
    this._requestSeq = 0;
    this._menuEl = null;

    const root = this.attachShadow({ mode: 'open' });
    root.adoptedStyleSheets = [sheet];
    this._input = document.createElement('input');
    this._input.type = 'text';
    this._input.autocomplete = 'off';
    root.appendChild(this._input);

    this._input.addEventListener('input', () => {
      clearTimeout(this._debounceTimer);
      const q = this._input.value.trim();
      this._debounceTimer = setTimeout(() => this._search(q), 250);
    });
    this._input.addEventListener('keydown', (e) => this._onKeydown(e));
    this._onDocMouseDown = (e) => {
      if (this._menuEl && !this._menuEl.contains(e.target) && e.target !== this._input) this._closeMenu();
    };
  }

  connectedCallback() {
    if (this.hasAttribute('placeholder')) this._input.placeholder = this.getAttribute('placeholder');
    if (this.hasAttribute('value')) this._input.value = this.getAttribute('value');
    if (this.hasAttribute('disabled')) this._input.disabled = true;
  }

  disconnectedCallback() {
    document.removeEventListener('mousedown', this._onDocMouseDown, true);
  }

  attributeChangedCallback(name, oldVal, newVal) {
    if (name === 'placeholder') this._input.placeholder = newVal || '';
    if (name === 'value' && this._input.value !== newVal) this._input.value = newVal || '';
    if (name === 'disabled') this._input.disabled = newVal !== null;
  }

  get value() { return this._input.value; }
  set value(v) { this._input.value = v == null ? '' : v; }

  get doctype() { return this.getAttribute('doctype') || ''; }
  get limit() { return parseInt(this.getAttribute('limit'), 10) || 8; }

  // Which field of a matched record becomes this.value on pick - defaults
  // to the same ['code','name','id'] preference order attachTypeahead uses
  // (see its own comment: master-data doctypes set `id` equal to `code` at
  // creation, so 'code' already resolves to a valid Link-checkable value).
  get valueFields() {
    const attr = this.getAttribute('value-fields');
    return attr ? attr.split(',').map(s => s.trim()) : ['code', 'name', 'id'];
  }

  _closeMenu() {
    if (this._menuEl) { this._menuEl.remove(); this._menuEl = null; }
    document.removeEventListener('mousedown', this._onDocMouseDown, true);
    this._items = [];
    this._activeIndex = -1;
  }

  _openMenu() {
    // Bug attachTypeahead itself once had (Stage 18.1): closing the menu to
    // clear stale results also wiped the just-fetched ones if _closeMenu
    // touched this._items - it deliberately doesn't, so this is safe.
    if (this._menuEl) { this._menuEl.remove(); this._menuEl = null; }
    if (this._items.length === 0) return;
    const menu = document.createElement('div');
    menu.className = 'menu';
    const rect = this._input.getBoundingClientRect();
    menu.style.left = `${rect.left}px`;
    menu.style.top = `${rect.bottom + 4}px`;
    menu.style.width = `${Math.max(rect.width, 180)}px`;
    this._items.forEach((doc) => {
      const row = document.createElement('div');
      row.className = 'item';
      row.textContent = this._label(doc);
      row.addEventListener('mousedown', (e) => { e.preventDefault(); this._pick(doc); });
      menu.appendChild(row);
    });
    this.shadowRoot.appendChild(menu);
    this._menuEl = menu;
    document.addEventListener('mousedown', this._onDocMouseDown, true);
  }

  _label(doc) {
    const code = doc.code || doc.id || '';
    const name = doc.name || '';
    return name && name !== code ? `${code} — ${name}` : (code || name);
  }

  _pick(doc) {
    let val = '';
    for (const f of this.valueFields) {
      if (doc[f] !== undefined && doc[f] !== null && doc[f] !== '') { val = doc[f]; break; }
    }
    this._input.value = val;
    this._closeMenuKeepFocus();
    this.dispatchEvent(new CustomEvent('change', { bubbles: true, composed: true, detail: { value: val, record: doc } }));
  }

  _closeMenuKeepFocus() {
    this._closeMenu();
    this._input.focus();
  }

  _highlight(idx) {
    if (!this._menuEl) return;
    const rows = this._menuEl.querySelectorAll('.item');
    rows.forEach(r => r.classList.remove('active'));
    if (idx >= 0 && rows[idx]) { rows[idx].classList.add('active'); rows[idx].scrollIntoView({ block: 'nearest' }); }
    this._activeIndex = idx;
  }

  _onKeydown(e) {
    if (!this._menuEl || this._items.length === 0) return;
    if (e.key === 'ArrowDown') { e.preventDefault(); this._highlight(Math.min(this._activeIndex + 1, this._items.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); this._highlight(Math.max(this._activeIndex - 1, 0)); }
    else if (e.key === 'Enter') {
      if (this._activeIndex >= 0) {
        e.preventDefault();
        e.stopImmediatePropagation();
        this._pick(this._items[this._activeIndex]);
      }
    } else if (e.key === 'Escape') { this._closeMenu(); }
  }

  async _search(q) {
    const seq = ++this._requestSeq;
    if (!q) { this._closeMenu(); return; }
    if (typeof window.apiFetch !== 'function') return;
    const res = await window.apiFetch(`/api/v1/doc/${this.doctype}?q=${encodeURIComponent(q)}&limit=${this.limit}`);
    if (seq !== this._requestSeq) return;
    if (!res || !res.ok) { this._closeMenu(); return; }
    this._items = await res.json();
    if (seq !== this._requestSeq) return;
    this._openMenu();
  }
}

customElements.define('erp-typeahead', ErpTypeahead);
