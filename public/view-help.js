// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
async function loadHelpIndex() {
  if (helpIndexCache) return helpIndexCache;
  const res = await apiFetch('/api/v1/help/index');
  if (!res || !res.ok) return null;
  helpIndexCache = await res.json();
  return helpIndexCache;
}

async function loadHelpSearchIndex() {
  if (helpSearchCache) return helpSearchCache;
  const res = await apiFetch('/api/v1/help/search-index');
  if (!res || !res.ok) return null;
  helpSearchCache = await res.json();
  return helpSearchCache;
}

// searchHelp scores a document by how many of the query's terms it contains,
// which is the whole ranking model. A term is matched as a prefix so typing
// "reserv" finds "reservation" - the alternative, exact terms only, makes a
// search box feel broken while you are still typing.
function searchHelp(index, query) {
  const terms = (query || '').toLowerCase().match(/[a-z0-9][a-z0-9_-]*/g) || [];
  if (terms.length === 0) return [];
  const scores = new Map();
  terms.forEach(term => {
    const matchedDocs = new Set();
    Object.keys(index.terms).forEach(indexed => {
      if (indexed.startsWith(term)) index.terms[indexed].forEach(doc => matchedDocs.add(doc));
    });
    matchedDocs.forEach(doc => scores.set(doc, (scores.get(doc) || 0) + 1));
  });
  return [...scores.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)
    .map(([doc, score]) => ({ ...index.docs[doc], score }));
}

// flattenHelpArticles gives the reading order the prev/next links follow - the
// sidebar's own order, so "next" always means the next thing in the sidebar.
function flattenHelpArticles(index) {
  const flat = [];
  (index.sections || []).forEach(section => (section.articles || []).forEach(article => flat.push(article)));
  return flat;
}

// 210.2: the sidebar mirrors the app's main menu. Module Handbooks are filed
// under the app's main modules (index.menu_modules, in menu order); the open
// article's group and module stay open and the article lists its own sections,
// the one you came for marked. Every other group folds, so the modules you are
// not in still show by name.
function helpTocHTML(article, anchor) {
  const groups = [];
  (article.headings || []).forEach(heading => {
    if (heading.level === 2) groups.push({ head: heading, subs: [] });
    else if (heading.level === 3 && groups.length) groups[groups.length - 1].subs.push(heading);
  });
  if (groups.length === 0) return '';
  const item = heading => `<li><a href="#${escapeHTMLText(heading.slug)}" data-anchor="${escapeHTMLText(heading.slug)}" class="kb-nav-anchor${heading.slug === anchor ? ' current' : ''}">${escapeHTMLText(heading.text)}</a></li>`;
  return `<ul class="kb-nav-toc">${groups.map(group => {
    const open = group.subs.length && (group.head.slug === anchor || group.subs.some(sub => sub.slug === anchor));
    return item(group.head) + (open ? `<li><ul class="kb-nav-toc">${group.subs.map(item).join('')}</ul></li>` : '');
  }).join('')}</ul>`;
}

function helpNavHTML(index, slug, anchor) {
  const current = flattenHelpArticles(index).find(article => article.slug === slug);
  const link = article => `<li><a href="/help/${encodeURIComponent(article.slug)}" data-slug="${escapeHTMLText(article.slug)}" class="kb-nav-link${article.slug === slug ? ' active' : ''}">${escapeHTMLText(article.title)}</a>${article.slug === slug ? helpTocHTML(article, anchor) : ''}</li>`;
  const order = index.menu_modules || [];
  const rank = name => (order.indexOf(name) + 1) || order.length + 1;
  return (index.sections || []).map(section => {
    const articles = section.articles || [];
    const byModule = articles.some(article => article.menu_module);
    let body = `<ul>${articles.map(link).join('')}</ul>`;
    if (byModule) {
      const modules = new Map();
      articles.forEach(article => {
        const name = article.menu_module || 'Other';
        if (!modules.has(name)) modules.set(name, []);
        modules.get(name).push(article);
      });
      body = [...modules.keys()].sort((a, b) => rank(a) - rank(b)).map(name =>
        `<details class="kb-nav-module"${current && current.menu_module === name ? ' open' : ''}><summary>${escapeHTMLText(name)}</summary><ul>${modules.get(name).map(link).join('')}</ul></details>`).join('');
    }
    const open = byModule || (current && current.section === section.name);
    return `<details class="kb-nav-section"${open ? ' open' : ''}><summary>${escapeHTMLText(section.name)}</summary>${body}</details>`;
  }).join('');
}

// markHelpAnchor scrolls the article to one section and marks it, in the page
// and in the sidebar - the landing spot of a help-dialog or error-code link.
function markHelpAnchor(holder, anchor) {
  holder.querySelectorAll('.kb-target').forEach(el => el.classList.remove('kb-target'));
  document.querySelectorAll('.kb-nav-anchor.current').forEach(el => el.classList.remove('current'));
  const target = anchor && holder.querySelector('#' + CSS.escape(anchor));
  if (!target) return false;
  target.classList.add('kb-target');
  document.querySelector(`.kb-nav-anchor[data-anchor="${CSS.escape(anchor)}"]`)?.classList.add('current');
  // Opened from the Help dialog, the article is built off-screen and moved into
  // the view afterwards (renderView), where a scroll made now is lost - so the
  // scroll waits for the move (about a second at most).
  const scroll = (tries = 0) => {
    if (!target.isConnected) return;
    if (holder.closest('#view-root') || tries > 60) target.scrollIntoView({ block: 'start' });
    else requestAnimationFrame(() => scroll(tries + 1));
  };
  scroll();
  return true;
}

// 210.4: snapshots carry no src. One is fetched through the authenticated help
// API when it scrolls near the screen, once per session, so an article costs
// nothing for the pictures nobody scrolls to.
const helpSnapshotURLs = new Map();
function loadHelpSnapshots(holder) {
  const images = [...holder.querySelectorAll('img[data-kb-img]:not([src])')];
  if (images.length === 0) return;
  const load = async img => {
    const name = img.getAttribute('data-kb-img');
    let url = helpSnapshotURLs.get(name);
    if (!url) {
      const res = await apiFetch('/api/v1/help/img/' + encodeURIComponent(name));
      if (!res || !res.ok) return;
      url = URL.createObjectURL(await res.blob());
      helpSnapshotURLs.set(name, url);
    }
    img.src = url;
  };
  if (!('IntersectionObserver' in window)) { images.forEach(load); return; }
  const observer = new IntersectionObserver(entries => entries.forEach(entry => {
    if (!entry.isIntersecting) return;
    observer.unobserve(entry.target);
    load(entry.target);
  }), { rootMargin: '300px' });
  images.forEach(img => observer.observe(img));
}

async function renderHelpView(container) {
  const index = await loadHelpIndex();
  if (!index) {
    container.innerHTML = `<div class="table-panel s-ddaf9e"><p>The Knowledge Center could not be loaded.</p></div>`;
    return;
  }
  const flat = flattenHelpArticles(index);
  if (!currentHelpSlug && flat.length > 0) currentHelpSlug = flat[0].slug;

  const layout = document.createElement('div');
  layout.className = 'kb-layout';
  layout.innerHTML = `
    <aside class="kb-sidebar">
      <div class="kb-search">
        <input type="search" id="kb-search-input" class="form-input" placeholder="Search help..." aria-label="Search help">
        <div id="kb-search-results" class="kb-search-results hidden"></div>
      </div>
      <nav id="kb-nav" aria-label="Knowledge Center sections"></nav>
    </aside>
    <article class="kb-article table-panel s-ddaf9e" id="kb-article"></article>`;
  container.appendChild(layout);

  const nav = layout.querySelector('#kb-nav');
  nav.innerHTML = helpNavHTML(index, currentHelpSlug, currentHelpAnchor);
  nav.addEventListener('click', event => {
    const section = event.target.closest('.kb-nav-anchor');
    if (section) {
      event.preventDefault();
      const holder = document.getElementById('kb-article');
      if (holder) markHelpAnchor(holder, section.getAttribute('data-anchor'));
      return;
    }
    const link = event.target.closest('.kb-nav-link');
    if (!link) return;
    event.preventDefault();
    openHelpArticle(link.getAttribute('data-slug'));
  });

  const searchInput = layout.querySelector('#kb-search-input');
  const searchResults = layout.querySelector('#kb-search-results');
  searchInput.addEventListener('input', async () => {
    const query = searchInput.value.trim();
    if (query.length < 2) { searchResults.classList.add('hidden'); return; }
    const searchIndex = await loadHelpSearchIndex();
    if (!searchIndex) return;
    const hits = searchHelp(searchIndex, query);
    searchResults.innerHTML = hits.length === 0
      ? `<div class="kb-search-empty">Nothing matched &ldquo;${escapeHTMLText(query)}&rdquo;.</div>`
      : hits.map(hit => `<a href="#" data-slug="${escapeHTMLText(hit.slug)}"><strong>${escapeHTMLText(hit.title)}</strong><span>${escapeHTMLText(hit.section)}</span></a>`).join('');
    searchResults.classList.remove('hidden');
  });
  searchResults.addEventListener('click', event => {
    const link = event.target.closest('a[data-slug]');
    if (!link) return;
    event.preventDefault();
    searchResults.classList.add('hidden');
    searchInput.value = '';
    openHelpArticle(link.getAttribute('data-slug'));
  });

  await renderHelpArticle(currentHelpSlug);
}

async function openHelpArticle(slug, anchor = '') {
  currentHelpSlug = slug;
  currentHelpAnchor = anchor;
  history.pushState({}, '', '/help/' + encodeURIComponent(slug));
  await renderHelpArticle(slug);
}
window.openHelpArticle = openHelpArticle;

// 86.1b: Back / Forward to a /help/<slug> entry changes only the path, which
// no hashchange sees - so render the article the address bar now names (and
// Help itself when Back comes from another screen). A hash naming another
// screen is the hashchange listener's.
if (!window.__helpPopstateWired) {
  window.__helpPopstateWired = true;
  window.addEventListener('popstate', () => {
    const m = location.pathname.match(/^\/help(?:\/(.*))?$/);
    const link = parseDeepLink();
    if (!m || (link && !(link.kind === 'view' && link.view === 'help'))) return;
    const slug = decodeURIComponent(m[1] || '');
    if (currentView !== 'help') { currentHelpSlug = slug; renderView('help'); return; }
    if (slug === currentHelpSlug) return;
    currentHelpSlug = slug;
    renderHelpArticle(slug);
  });
}

// Stage 39.9 - per-browser dedup for the "Was this helpful?" widget below.
// A page reload that lands on the same article should not ask again.
function helpFeedbackAlreadyGiven(slug) {
  try { return !!localStorage.getItem(`erp_kb_feedback_${slug}`); } catch { return false; }
}
function markHelpFeedbackGiven(slug) {
  try { localStorage.setItem(`erp_kb_feedback_${slug}`, '1'); } catch { /* best-effort only */ }
}

async function renderHelpArticle(slug) {
  const holder = document.getElementById('kb-article');
  if (!holder) return;
  if (!slug) { holder.innerHTML = '<p class="text-muted">Select an article from the list.</p>'; return; }
  const res = await apiFetch(`/api/v1/help/article/${encodeURIComponent(slug)}`);
  if (!res || !res.ok) {
    holder.innerHTML = `<p class="text-muted">That article could not be loaded.</p>`;
    return;
  }
  const article = await res.json();
  const index = await loadHelpIndex();
  const flat = flattenHelpArticles(index || { sections: [] });
  const position = flat.findIndex(entry => entry.slug === slug);
  const previous = position > 0 ? flat[position - 1] : null;
  const next = position >= 0 && position < flat.length - 1 ? flat[position + 1] : null;

  const toc = (article.headings || []).filter(heading => heading.level === 2);
  holder.innerHTML = `
    <nav class="kb-breadcrumb" aria-label="Breadcrumb">
      <a href="/help" ${actionAttrs('openHelpArticle', [flat[0] ? flat[0].slug : ''], { prevent: true })}>Help</a>
      <span>/</span><span>${escapeHTMLText(article.section || '')}</span>
      <span>/</span><span>${escapeHTMLText(article.title || '')}</span>
    </nav>
    ${toc.length > 1 ? `<details class="kb-toc" open><summary>Contents</summary><ul>${toc.map(h => `<li><a href="#${escapeHTMLText(h.slug)}">${escapeHTMLText(h.text)}</a></li>`).join('')}</ul></details>` : ''}
    <div class="kb-body">${article.html}</div>
    <footer class="kb-footer">
      ${article.last_verified ? `<span class="text-muted">Last verified ${escapeHTMLText(article.last_verified)}.</span>` : ''}
      <div class="kb-feedback">${helpFeedbackAlreadyGiven(slug)
        ? '<span class="text-muted">Thanks for the feedback.</span>'
        : 'Was this helpful? <button type="button" class="btn btn-outline btn-sm" data-helpful="Yes">Yes</button> <button type="button" class="btn btn-outline btn-sm" data-helpful="No">No</button>'}</div>
      <div class="kb-pager">
        ${previous ? `<a href="#" data-slug="${escapeHTMLText(previous.slug)}">&larr; ${escapeHTMLText(previous.title)}</a>` : '<span></span>'}
        ${next ? `<a href="#" data-slug="${escapeHTMLText(next.slug)}">${escapeHTMLText(next.title)} &rarr;</a>` : '<span></span>'}
      </div>
    </footer>`;
  holder.querySelectorAll('.kb-pager a[data-slug]').forEach(link => {
    link.addEventListener('click', event => { event.preventDefault(); openHelpArticle(link.getAttribute('data-slug')); });
  });
  // Stage 39.9 - "Was this helpful?" feedback. Submits straight to the
  // existing generic-document API (POST /api/v1/doc/{doctype}) as an
  // ordinary HelpArticleFeedback record - no bespoke endpoint. Dedup is a
  // per-browser localStorage flag, not a server-side one-vote rule: this is
  // signal for an author deciding what to rewrite, not a poll that needs to
  // resist ballot-stuffing.
  holder.querySelector('.kb-feedback')?.addEventListener('click', async event => {
    const button = event.target.closest('button[data-helpful]');
    if (!button) return;
    const feedbackEl = button.closest('.kb-feedback');
    const res = await apiFetch('/api/v1/doc/HelpArticleFeedback', {
      method: 'POST',
      body: JSON.stringify({ article: slug, helpful: button.getAttribute('data-helpful') })
    });
    if (res && res.ok) {
      markHelpFeedbackGiven(slug);
      feedbackEl.innerHTML = '<span class="text-muted">Thanks for the feedback.</span>';
    }
  });
  // Cross-references inside the article body. The renderer turns a relative
  // `other-article.md` link into `/help/other-article`, which is a real URL the
  // server would serve - so this is an optimisation, not a fix: it keeps an
  // in-article link as fast as a sidebar click instead of reloading the app.
  // Modified clicks are left alone so ctrl/middle-click still opens a new tab.
  // A `#section` link (an error code, a 210.1 help-dialog link) opens the
  // article at that section instead of its top.
  holder.querySelector('.kb-body')?.addEventListener('click', event => {
    const link = event.target.closest('a[href^="/help/"]');
    if (!link || event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    const [target, anchor] = link.getAttribute('href').slice('/help/'.length).split('#');
    if (!target) return;
    event.preventDefault();
    if (decodeURIComponent(target) === slug) { markHelpAnchor(holder, anchor || ''); return; }
    openHelpArticle(decodeURIComponent(target), anchor || '');
  });
  // In-page section links scroll here rather than touch the URL hash, which
  // belongs to the app's own router. The holder outlives each article, so the
  // listener goes on once.
  if (!holder.dataset.kbAnchors) {
    holder.dataset.kbAnchors = '1';
    holder.addEventListener('click', event => {
      const link = event.target.closest('a[href^="#"]');
      if (!link) return;
      event.preventDefault();
      markHelpAnchor(holder, decodeURIComponent(link.getAttribute('href').slice(1)));
    });
  }
  // 210.2: the sidebar follows the article - its module opens, its sections list.
  const nav = document.getElementById('kb-nav');
  if (nav && index) {
    nav.innerHTML = helpNavHTML(index, slug, currentHelpAnchor);
    nav.querySelector('.kb-nav-link.active')?.scrollIntoView({ block: 'nearest' });
  }
  const anchor = currentHelpAnchor;
  currentHelpAnchor = '';
  if (!markHelpAnchor(holder, anchor)) {
    holder.scrollTop = 0;
    window.scrollTo(0, 0);
  }
  loadHelpSnapshots(holder);
  playHelpSteps(holder);
}

// 290.4: a ```steps block plays as a "How it works" step-through. The player
// loads only for an article that has one; should it fail, the block stays
// readable as plain text.
async function playHelpSteps(holder) {
  const blocks = [...holder.querySelectorAll('pre > code.language-steps')];
  if (blocks.length === 0) return;
  try {
    const { mountHelpSteps } = await import('./view-help-steps.js' + new URL(import.meta.url).search);
    blocks.forEach(code => mountHelpSteps(code.parentElement));
  } catch { /* the block stays as text */ }
}

// openHelpDrawer (Stage 39.5) shows the article(s) mapped to a screen, over the
// screen itself. The mapping comes from each article's own `screens:`
// frontmatter via the index's screen_map, so there is no second mapping file to
// keep in sync with the articles.
// menuModuleOf names the main-menu module a screen sits in, read from the
// screen's own menu entry (not the highlight, which a jump from another
// screen leaves behind). Only a module the Knowledge Center files handbooks
// under counts, so a Favourites copy of the entry is passed over.
function menuModuleOf(screenID, known) {
  const view = String(screenID || '');
  const selectors = [`.sidebar-menu [data-go="${CSS.escape(view)}"]`];
  if (view === 'doctype-table' && currentDoctype) selectors.unshift(`.sidebar-menu [data-doctype="${CSS.escape(currentDoctype)}"]`);
  const menuID = (typeof STATIC_VIEW_MENU_IDS === 'object' && STATIC_VIEW_MENU_IDS[view]) || 'menu-' + view;
  selectors.push('#' + CSS.escape(menuID));
  for (const item of document.querySelectorAll(selectors.join(','))) {
    const label = item.closest('.has-flyout')?.querySelector('.menu-item-group span')?.textContent.trim();
    if (label && known.includes(label)) return label;
  }
  return '';
}

async function openHelpDrawer(screenID) {
  // Stage 135.7: the drawer is styled by kb.css, which the Help screen's
  // registry entry loads; the drawer opens from any screen, so load it here too.
  const [index] = await Promise.all([loadHelpIndex(), loadViewStyles('kb.css')]);
  if (!index) {
    showCustomAlert('The Knowledge Center could not be loaded.', 'Help');
    return;
  }
  // Every master list is the one generic `doctype-table` screen, so that id
  // alone named no screen: articles claiming it all matched every list, and
  // the alphabetically first (Channel Connectors) opened on Bin, Dock Doors
  // and Print Template alike. A list asks for its own doctype first, then the
  // handbook of the module that doctype belongs to.
  const map = index.screen_map || {};
  let keys = [String(screenID || '').toLowerCase()];
  if (keys[0] === 'doctype-table' && currentDoctype) {
    const meta = (state.activeDoctypes || []).find(d => d.name === currentDoctype);
    keys = ['doctype:' + currentDoctype.toLowerCase(), meta && meta.module ? 'module:' + meta.module.toLowerCase() : ''];
  }
  const key = keys.find(k => (map[k] || []).length);
  const slugs = map[key] || [];
  // 210.1: an article's `screen_sections:` names the section that explains
  // this screen. The dialog shows just that section and its button opens the
  // Knowledge Center there - the full lesson, under the screen's main module -
  // never on the Knowledge Center's first page. The handbook filed under the
  // main-menu module the screen sits in comes first when it has a section
  // for the screen (GRN is in Procurement, not Inventory); then the screen
  // map's own first choice if it has one; then that module's handbook; then
  // the screen map's first choice.
  const sections = ((index.screen_sections || {})[key] || []).map(t => t.split('#'));
  const menuModule = menuModuleOf(screenID, index.menu_modules || []);
  const filedIn = new Map(flattenHelpArticles(index).map(a => [a.slug, a.menu_module || '']));
  const inMenu = s => !!menuModule && filedIn.get(s) === menuModule;
  const hasSection = s => sections.some(t => t[0] === s);
  const slug = slugs.find(s => inMenu(s) && hasSection(s)) || (hasSection(slugs[0]) && slugs[0]) || slugs.find(inMenu) || slugs[0];
  const anchors = sections.filter(t => t[0] === slug).map(t => t[1]);
  const fullHref = slug ? '/help/' + encodeURIComponent(slug) + (anchors[0] ? '#' + anchors[0] : '') : '/help';

  document.getElementById('kb-drawer')?.remove();
  // 48.7.7: a dialog for screen readers, closed by Escape, focus in on open and back on close.
  const returnFocus = document.activeElement;
  const drawer = document.createElement('div');
  drawer.id = 'kb-drawer';
  drawer.className = 'modal-overlay open';
  drawer.setAttribute('role', 'dialog');
  drawer.setAttribute('aria-modal', 'true');
  drawer.setAttribute('aria-labelledby', 'kb-drawer-title');
  drawer.innerHTML = `
    <div class="modal-container kb-drawer-container">
      <div class="modal-header">
        <h3 class="modal-title" id="kb-drawer-title">Help</h3>
        <button type="button" class="modal-close" aria-label="Close">&times;</button>
      </div>
      <div class="modal-body" id="kb-drawer-body"><p class="text-muted">Loading&hellip;</p></div>
      <div class="modal-footer">
        <a class="btn btn-outline kb-drawer-full" href="${escapeHTMLText(fullHref)}">Open the full Knowledge Center</a>
        <button type="button" class="btn btn-secondary">Close</button>
      </div>
    </div>`;
  document.body.appendChild(drawer);
  const close = () => {
    drawer.remove();
    if (returnFocus && returnFocus.focus) returnFocus.focus();
  };
  drawer.querySelector('.modal-close').addEventListener('click', close);
  drawer.querySelector('.btn-secondary').addEventListener('click', close);
  drawer.addEventListener('keydown', e => { if (e.key === 'Escape') { e.stopPropagation(); close(); } });
  drawer.querySelector('.modal-close').focus();
  // Every /help link in the dialog - the button, "Also for this screen", a
  // cross-reference in the text - closes it and opens the Knowledge Center at
  // that article and section, without reloading the app.
  drawer.addEventListener('click', event => {
    const link = event.target.closest('a[href^="/help"]');
    if (!link || event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    const [path, anchor] = link.getAttribute('href').split('#');
    close();
    currentHelpSlug = decodeURIComponent(path.slice('/help/'.length));
    currentHelpAnchor = anchor || '';
    history.pushState({}, '', path);
    renderView('help');
  });

  const body = drawer.querySelector('#kb-drawer-body');
  if (!slug) {
    // Say plainly that this screen has no article yet rather than showing an
    // arbitrary one - a wrong article is worse than an honest gap.
    body.innerHTML = `<p>No help article is mapped to this screen yet.</p>
      <p class="text-muted">Open the full Knowledge Center to search everything, or ask an administrator to have this screen documented.</p>`;
    return;
  }
  const res = await apiFetch(`/api/v1/help/article/${encodeURIComponent(slug)}`);
  if (!res || !res.ok) { body.innerHTML = '<p class="text-muted">That article could not be loaded.</p>'; return; }
  const article = await res.json();
  const others = slugs.filter(s => s !== slug);
  const titleOf = s => (flattenHelpArticles(index).find(a => a.slug === s) || {}).title || s;
  body.innerHTML = `
    <h4 class="s-249d01">${escapeHTMLText(article.title)}</h4>
    <div class="kb-body">${helpSectionHTML(article.html, anchors)}</div>
    ${others.length > 0 ? `<p class="text-muted s-1b0f49">Also for this screen: ${others.map(s => `<a href="/help/${encodeURIComponent(s)}">${escapeHTMLText(titleOf(s))}</a>`).join(', ')}</p>` : ''}`;
  playHelpSteps(body);
}

// helpSectionHTML keeps only the named sections of an article: each heading
// with everything after it up to the next heading of the same or a higher
// level. With no section named, or none found, the whole article stays.
function helpSectionHTML(html, anchors) {
  if (!anchors || anchors.length === 0) return html;
  const doc = document.createElement('div');
  doc.innerHTML = html;
  const out = [];
  anchors.forEach(anchor => {
    const head = doc.querySelector('#' + CSS.escape(anchor));
    if (!head || !/^H[1-6]$/.test(head.tagName)) return;
    const level = Number(head.tagName[1]);
    out.push(head.outerHTML);
    for (let el = head.nextElementSibling; el; el = el.nextElementSibling) {
      if (/^H[1-6]$/.test(el.tagName) && Number(el.tagName[1]) <= level) break;
      out.push(el.outerHTML);
    }
  });
  return out.length ? out.join('') : html;
}
window.openHelpDrawer = openHelpDrawer;

// Window load init

export { loadHelpIndex, loadHelpSearchIndex, searchHelp, flattenHelpArticles, renderHelpView, openHelpArticle, helpFeedbackAlreadyGiven, markHelpFeedbackGiven, renderHelpArticle, openHelpDrawer };
