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

async function renderHelpView(container) {
  const index = await loadHelpIndex();
  if (!index) {
    container.innerHTML = `<div class="table-panel" style="padding:24px;"><p>The Knowledge Center could not be loaded.</p></div>`;
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
    <article class="kb-article table-panel" id="kb-article" style="padding:24px;"></article>`;
  container.appendChild(layout);

  const nav = layout.querySelector('#kb-nav');
  nav.innerHTML = (index.sections || []).map(section => `
    <div class="kb-nav-section">
      <h4>${escapeHTMLText(section.name)}</h4>
      <ul>${(section.articles || []).map(article =>
        `<li><a href="/help/${encodeURIComponent(article.slug)}" data-slug="${escapeHTMLText(article.slug)}" class="kb-nav-link${article.slug === currentHelpSlug ? ' active' : ''}">${escapeHTMLText(article.title)}</a></li>`).join('')}
      </ul>
    </div>`).join('');
  nav.addEventListener('click', event => {
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

async function openHelpArticle(slug) {
  currentHelpSlug = slug;
  history.pushState({}, '', '/help/' + encodeURIComponent(slug));
  document.querySelectorAll('.kb-nav-link').forEach(link => {
    link.classList.toggle('active', link.getAttribute('data-slug') === slug);
  });
  await renderHelpArticle(slug);
}
window.openHelpArticle = openHelpArticle;

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
  holder.querySelector('.kb-body')?.addEventListener('click', event => {
    const link = event.target.closest('a[href^="/help/"]');
    if (!link || event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    const target = link.getAttribute('href').slice('/help/'.length);
    if (!target || target.includes('#')) return;
    event.preventDefault();
    openHelpArticle(decodeURIComponent(target));
  });
  holder.scrollTop = 0;
}

// openHelpDrawer (Stage 39.5) shows the article(s) mapped to a screen, over the
// screen itself. The mapping comes from each article's own `screens:`
// frontmatter via the index's screen_map, so there is no second mapping file to
// keep in sync with the articles.
async function openHelpDrawer(screenID) {
  const index = await loadHelpIndex();
  if (!index) {
    showCustomAlert('The Knowledge Center could not be loaded.', 'Help');
    return;
  }
  const slugs = (index.screen_map || {})[String(screenID || '').toLowerCase()] || [];

  document.getElementById('kb-drawer')?.remove();
  const drawer = document.createElement('div');
  drawer.id = 'kb-drawer';
  drawer.className = 'modal-overlay open';
  drawer.innerHTML = `
    <div class="modal-container kb-drawer-container">
      <div class="modal-header">
        <h3 class="modal-title">Help</h3>
        <button type="button" class="modal-close" aria-label="Close">&times;</button>
      </div>
      <div class="modal-body" id="kb-drawer-body"><p class="text-muted">Loading&hellip;</p></div>
      <div class="modal-footer">
        <a class="btn btn-outline" href="/help">Open the full Knowledge Center</a>
        <button type="button" class="btn btn-secondary">Close</button>
      </div>
    </div>`;
  document.body.appendChild(drawer);
  const close = () => drawer.remove();
  drawer.querySelector('.modal-close').addEventListener('click', close);
  drawer.querySelector('.btn-secondary').addEventListener('click', close);

  const body = drawer.querySelector('#kb-drawer-body');
  if (slugs.length === 0) {
    // Say plainly that this screen has no article yet rather than showing an
    // arbitrary one - a wrong article is worse than an honest gap.
    body.innerHTML = `<p>No help article is mapped to this screen yet.</p>
      <p class="text-muted">Open the full Knowledge Center to search everything, or ask an administrator to have this screen documented.</p>`;
    return;
  }
  const res = await apiFetch(`/api/v1/help/article/${encodeURIComponent(slugs[0])}`);
  if (!res || !res.ok) { body.innerHTML = '<p class="text-muted">That article could not be loaded.</p>'; return; }
  const article = await res.json();
  const others = slugs.slice(1);
  body.innerHTML = `
    <h4 style="margin:0 0 12px;">${escapeHTMLText(article.title)}</h4>
    <div class="kb-body">${article.html}</div>
    ${others.length > 0 ? `<p class="text-muted" style="margin-top:16px;">Also for this screen: ${others.map(slug => `<a href="/help/${encodeURIComponent(slug)}">${escapeHTMLText(slug)}</a>`).join(', ')}</p>` : ''}`;
}
window.openHelpDrawer = openHelpDrawer;

// Window load init

export { loadHelpIndex, loadHelpSearchIndex, searchHelp, flattenHelpArticles, renderHelpView, openHelpArticle, helpFeedbackAlreadyGiven, markHelpFeedbackGiven, renderHelpArticle, openHelpDrawer };
