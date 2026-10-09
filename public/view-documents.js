// BLD-041: native ES module, loaded on first authorized view visit.
// Shared app services resolve from the classic shell; these named exports
// are published by the loader for legacy cross-view calls.
async function renderDocTableView(container) {
  const doctypeAtRequestTime = currentDoctype;
  const pageAtRequestTime = currentTablePage;
  const searchAtRequestTime = currentSearchQuery;
  const mySeq = ++docTableRequestSeq;
  const cacheKey = `doctable:${doctypeAtRequestTime}:${pageAtRequestTime}:${searchAtRequestTime}`;
  const cached = swrFetch(cacheKey, () => fetchDocTableData(doctypeAtRequestTime, pageAtRequestTime, searchAtRequestTime), (fresh) => {
    // Only apply if the user hasn't navigated to a different doctype/view,
    // or changed page/search, by the time this background revalidation
    // resolves (docTableRequestSeq is bumped by every one of those, not
    // just by re-entering this view).
    if (docTableRequestSeq !== mySeq || currentView !== 'doctype-table') return;
    state.activeDocFields = fresh.fields;
    state.docData = fresh.records;
    state.docTotal = fresh.total;
    renderDocTable();
  });

  if (cached) {
    state.activeDocFields = cached.fields;
    state.docData = cached.records;
    state.docTotal = cached.total;
  } else {
    const metaRes = await apiFetch(`/api/v1/doc/${currentDoctype}/meta`);
    if (!metaRes) return;
    if (!metaRes.ok) {
      const msg = await getErrorMessage(metaRes, `Failed to load schema for ${getDoctypeLabel(currentDoctype)}.`);
      renderErrorPanel(container, msg, () => renderView('doctype-table'));
      return;
    }
    state.activeDocFields = await metaRes.json();

    const params = docTableListParams(pageAtRequestTime, searchAtRequestTime);
    const dataRes = await apiFetch(`/api/v1/doc/${currentDoctype}?${params.toString()}`);
    if (!dataRes) return;
    if (!dataRes.ok) {
      const msg = await getErrorMessage(dataRes, `Failed to load records for ${getDoctypeLabel(currentDoctype)}.`);
      renderErrorPanel(container, msg, () => renderView('doctype-table'));
      return;
    }
    state.docData = await dataRes.json();
    const totalHeader = dataRes.headers.get('X-Total-Count');
    const offset = (pageAtRequestTime - 1) * itemsPerPage;
    state.docTotal = totalHeader !== null ? (parseInt(totalHeader, 10) || 0) : offset + state.docData.length;
  }
  bulkSelectedDocIDs = new Set();

  // Stay inside the PIM shell (title + sub-tab bar) for doctypes reached via
  // a PIM tab, instead of this view's own header replacing it outright -
  // otherwise clicking e.g. "Product Families" feels like it left PIM
  // entirely for an unrelated full-page master list.
  if (PIM_DOCTYPES.has(currentDoctype)) {
    // Setup's direct Product Family/Attribute links can be the first PIM
    // route a user opens. Since BLD-041 split PIM into its own native module,
    // its shell header is not a global until that module has been imported.
    // Load only the shared header here rather than making every Setup record
    // screen eagerly fetch the PIM module.
    try {
      const pimModule = await loadViewModule('/view-pim.js?v=1');
      pimModule.renderPIMShellHeader(container);
    } catch (error) {
      console.error('[BLD-041] PIM shell failed to load for a Setup record', error);
      renderErrorPanel(container, 'The Product screen could not be started. Check your connection and try again.', () => renderView('doctype-table'));
      return;
    }
  }

  // Stage 2026-09-22: shown only when this list was reached via a
  // "create the missing master" shortcut from inside another screen (e.g.
  // GRN's Item picker) - see quickCreateReturn's declaration.
  const showQuickCreateBack = quickCreateReturn && quickCreateReturn.forDoctype === currentDoctype;

  // BLD-037: this subtitle used to read literally "Pluggable module
  // metadata records database" for every doctype - an internal/technical
  // phrase with no real per-doctype description behind it (doctype_meta has
  // no description column - engines/doctype.go's GetDocTypes), left over
  // from whenever this generic list view was first built. Every
  // purpose-built screen's own subtitle (Home's quick actions, POS,
  // Finance/GL, ...) is a plain-language one-liner; this generic fallback
  // path had drifted from that convention.
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      ${showQuickCreateBack ? `<a href="#" id="quick-create-back" class="empty-state-link" style="display:inline-block; margin-bottom:6px;">&larr; Back to ${escapeHTMLText(quickCreateReturn.label || getTranslatedLabel(quickCreateReturn.view))}</a>` : ''}
      <h1 class="page-title">${escapeHTMLText(getDoctypeLabel(currentDoctype))}</h1>
      <p class="page-subtitle">${currentDoctype === 'PasswordResetRequest'
        ? 'Password reset history. Start a reset from Users; privileged accounts require a second administrator’s approval.'
        : 'View and manage these records.'}</p>
    </div>
    <div style="display:flex; gap: 8px;">
      ${currentDoctype === 'PasswordResetRequest' ? (isMenuRuleVisible(MENU_PERMISSION_MAP['menu-users'])
        ? '<button class="btn btn-primary" id="doc-reset-users-button">Open Users for password reset</button>'
        : '<span class="page-header-note">Contact an administrator to request a password reset.</span>') : canCreateDoctype(currentDoctype) ? `
      <button class="btn btn-outline" id="doc-import-button">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        <span>Bulk Import</span>
      </button>
      <button class="btn btn-primary" id="doc-create-button">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
        <span>New ${escapeHTMLText(getDoctypeLabel(currentDoctype))}</span>
      </button>` : `
      <span class="page-header-note" title="Your role has read access to this record type but not create access. Ask an administrator to grant it under Admin &raquo; Roles.">Read-only for your role</span>`}
    </div>
  `;
  container.appendChild(header);
  header.querySelector('#quick-create-back')?.addEventListener('click', event => {
    event.preventDefault();
    returnFromQuickCreate();
  });
  header.querySelector('#doc-import-button')?.addEventListener('click', openImportModal);
  header.querySelector('#doc-create-button')?.addEventListener('click', () => openDynamicModal());
  header.querySelector('#doc-reset-users-button')?.addEventListener('click', () => renderView('users'));

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <input type="text" id="doc-table-search" placeholder="Search table..." value="${escapeHTMLText(currentSearchQuery)}">
      </div>
      ${currentDoctype === 'Item' ? `<button class="btn btn-outline" id="pim-group-actions-btn" title="Act on a saved Product Group instead of hand-picking rows">Group Actions</button>` : ''}
      ${isPIMBulkEditDoctype() ? `<div class="bulk-edit-bar hidden" id="pim-bulk-edit-bar"><span id="pim-bulk-selection-count">0 selected</span><button class="btn btn-outline" id="pim-bulk-edit-button" disabled>Edit Selected</button>${currentDoctype === 'ProductContent' ? `<button class="btn btn-primary" id="pim-bulk-approve-button" disabled>Approve Selected</button><button class="btn btn-outline" id="pim-bulk-reject-button" disabled>Reject Selected</button>` : ''}</div>` : ''}
    </div>
    <div class="table-wrapper" id="doc-table-wrapper"></div>
    <div class="pagination" id="doc-table-pagination"></div>
  `;
  container.appendChild(panel);
  panel.querySelector('#doc-table-search')?.addEventListener('input', handleTableSearch);
  panel.querySelector('#pim-group-actions-btn')?.addEventListener('click', openPIMProductGroupActionsModal);
  panel.querySelector('#pim-bulk-edit-button')?.addEventListener('click', openPIMBulkEditModal);
  panel.querySelector('#pim-bulk-approve-button')?.addEventListener('click', () => bulkDecideProductContent('Approved'));
  panel.querySelector('#pim-bulk-reject-button')?.addEventListener('click', () => bulkDecideProductContent('Rejected'));

  renderDocTable();
}

window.handleTableSearch = function(e) {
  currentSearchQuery = e.target.value.toLowerCase();
  currentTablePage = 1;
  // BLD-034: changing the filter clears the selection rather than silently
  // carrying it forward - at real table scale, a selection made against one
  // search can otherwise include rows the user can no longer see at all
  // (outside the new filtered view) and would still be acted on by a bulk
  // action, an invisible-scope surprise that only gets more likely the
  // bigger the table. Paging within the SAME search still preserves
  // selection across pages, unchanged.
  bulkSelectedDocIDs = new Set();
  updatePIMBulkEditBar();
  // Search is now a real server round trip (SQL, across every field - see
  // docTableListParams/handlers_core_doc_engine.go) instead of a client-side
  // .filter() over an already-fetched array, so firing one per keystroke
  // would flood the server; debounced, with docTableRequestSeq (bumped
  // inside refreshDocTablePage) guarding the rare case a slow earlier
  // request outlives a faster later one.
  clearTimeout(docTableSearchDebounce);
  docTableSearchDebounce = setTimeout(() => {
    refreshDocTablePage();
    saveNavState();
  }, 300);
};

// BLD-034: re-fetches just the current page's rows (not schema - fields
// don't change page to page) for a page or search change on an
// already-open doctype-table view. No SWR/sessionStorage caching here
// deliberately: paging deep into a large table would otherwise leave one
// cache entry per page ever visited in the session for little benefit (a
// user paging forward rarely returns to the same page) - unlike the single
// first-entry fetch in renderDocTableView, which SWR still covers.
async function refreshDocTablePage() {
  const doctype = currentDoctype;
  const page = currentTablePage;
  const search = currentSearchQuery;
  const mySeq = ++docTableRequestSeq;
  const focusedId = document.activeElement && document.activeElement.id;

  docTableLoading = true;
  renderDocTable();

  const params = docTableListParams(page, search);
  const dataRes = await apiFetch(`/api/v1/doc/${doctype}?${params.toString()}`);
  // Stale if the user changed doctype/page/search/view again while this was
  // in flight - docTableRequestSeq's job, same guard shape as the SWR
  // revalidation callbacks above.
  if (docTableRequestSeq !== mySeq || currentView !== 'doctype-table' || currentDoctype !== doctype) return;
  docTableLoading = false;
  if (!dataRes) { renderDocTable(); return; }
  if (!dataRes.ok) {
    await showApiError(dataRes, `Failed to load records for ${getDoctypeLabel(doctype)}.`);
    renderDocTable();
    return;
  }
  state.docData = await dataRes.json();
  const totalHeader = dataRes.headers.get('X-Total-Count');
  const offset = (page - 1) * itemsPerPage;
  state.docTotal = totalHeader !== null ? (parseInt(totalHeader, 10) || 0) : offset + state.docData.length;
  renderDocTable();

  // Keyboard focus (BLD-034/MC-083 "keyboard scroll/focus is usable"): a
  // Tab-focused Previous/Next click used to lose focus to <body> every page
  // change, since the whole pagination footer is rebuilt from scratch -
  // same class of gap BLD-010/011 already fixed for dialogs. Restore it to
  // the equivalent button, or the other one if this page is now an edge.
  if (focusedId === 'doc-table-prev-btn' || focusedId === 'doc-table-next-btn') {
    const el = document.getElementById(focusedId);
    const target = el && !el.disabled ? el : (document.getElementById('doc-table-prev-btn') || document.getElementById('doc-table-next-btn'));
    if (target) target.focus();
  }
}

function renderDocTable() {
  const wrapper = document.getElementById('doc-table-wrapper');
  const paginator = document.getElementById('doc-table-pagination');
  if (!wrapper) return;

  // BLD-034: state.docData is already exactly one page of server-filtered/
  // paginated rows (see refreshDocTablePage/renderDocTableView) - no more
  // client-side filtering or slicing over a locally-fetched batch here.
  const items = state.docData;
  const total = state.docTotal;
  const pages = Math.max(1, Math.ceil(total / itemsPerPage));
  const start = (currentTablePage - 1) * itemsPerPage;
  const end = Math.min(start + items.length, total);
  const bulkEditingEnabled = isPIMBulkEditDoctype();

  let tableHTML = `
    <table>
      <thead>
        <tr>
          ${bulkEditingEnabled ? `<th style="width: 42px;"><input type="checkbox" aria-label="Select all visible records" data-pim-select-page ${items.length > 0 && items.every(item => bulkSelectedDocIDs.has(item.id)) ? 'checked' : ''}></th>` : ''}
          ${state.activeDocFields.map(f => `<th>${escapeHTMLText(getTranslatedLabel(f.label))}</th>`).join('')}
          <th class="row-actions-cell" style="text-align: right;">Actions</th>
        </tr>
      </thead>
      <tbody>
  `;

  if (items.length === 0) {
    // Stage 30.5.2: this one placeholder serves every one of the 50+ generic
    // record lists, so it is the highest-leverage empty state in the app -
    // and "No records found." was equally uninformative whether the list was
    // genuinely empty or the user had simply typed a search that matched
    // nothing. Those are different problems with different next steps.
    const label = getDoctypeLabel(currentDoctype);
    const emptyMsg = currentSearchQuery
      ? `No ${label} records match &ldquo;${escapeHTMLText(currentSearchQuery)}&rdquo;. Clear the search box above to see all of them.`
      : `No ${label} records yet. Use <b>New ${label}</b> above to create the first one, or <b>Bulk Import</b> to load a CSV.`;
    tableHTML += `<tr><td colspan="${state.activeDocFields.length + 1 + (bulkEditingEnabled ? 1 : 0)}" class="text-center py-8 text-muted">${emptyMsg}</td></tr>`;
  } else {
    items.forEach(row => {
      tableHTML += `<tr>`;
      if (bulkEditingEnabled) {
        tableHTML += `<td><input type="checkbox" aria-label="Select ${escapeHTMLText(row.id)}" data-pim-doc-id="${escapeHTMLText(row.id)}" ${bulkSelectedDocIDs.has(row.id) ? 'checked' : ''}></td>`;
      }
      state.activeDocFields.forEach(f => {
        const val = row[f.fieldname] || '';
        if (f.fieldname === 'status') {
          const cls = val === 'Active' ? 'badge-success' : 'badge-secondary';
          tableHTML += `<td><span class="badge ${cls}">${escapeHTMLText(val)}</span></td>`;
        } else if (f.fieldtype === 'Link' && val && f.options) {
          // Stage 57.1: marked as a reference, so the shell's name sweep shows
          // the record's name here (the copy chip still copies the code).
          tableHTML += `<td data-link-doctype="${escapeHTMLText(f.options)}" data-link-ref="${escapeHTMLText(val)}">${copyableCell(val, val)}</td>`;
        } else {
          tableHTML += `<td>${copyableCell(val, val)}</td>`;
        }
      });
      const showHistory = TAXONOMY_HISTORY_DOCTYPES.has(currentDoctype);
      // Stage 26.3.2: Purchase Requisition has no bespoke workbench (unlike
      // GRN) - its schema is flat enough that the generic doctype-table
      // form already covers create/edit. The two gaps a plain form/table
      // can't cover on its own are submitting into the (already-existing,
      // Stage 17.7) approval flow, and the post-approval conversion action -
      // both added here as row actions, same extension point as
      // showHistory above.
      const prActions = currentDoctype === 'PurchaseRequisition'
        ? (row.status === 'Draft'
            ? `<button class="action-btn" title="Submit for Approval" aria-label="Submit ${escapeHTMLText(row.id)} for approval" style="margin-right:4px;" data-doc-action="submit-requisition" data-doc-id="${escapeHTMLText(row.id)}">
                 <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
               </button>`
            : row.status === 'Approved'
              ? `<button class="action-btn" title="Convert to RFQ" style="margin-right:4px;" data-doc-action="convert-requisition" data-doc-id="${escapeHTMLText(row.id)}" data-target="RFQ">RFQ</button>
                 <button class="action-btn" title="Convert to Purchase Order" style="margin-right:4px;" data-doc-action="convert-requisition" data-doc-id="${escapeHTMLText(row.id)}" data-target="PurchaseOrder">PO</button>`
              : '')
        // Stage 26.9.7: QC gate - a QualityInspection is a plain flat-schema
        // Transaction doctype (same "no bespoke workbench needed" shape as
        // PurchaseRequisition above); the only gap the generic form/table
        // can't cover is submitting into the already-existing approval flow.
        // Approve/Reject itself happens on the existing Approvals inbox
        // screen, not here.
        : currentDoctype === 'QualityInspection' && row.status === 'Draft'
        ? `<button class="action-btn" title="Submit for Approval" aria-label="Submit ${escapeHTMLText(row.id)} for approval" style="margin-right:4px;" data-doc-action="submit-quality-inspection" data-doc-id="${escapeHTMLText(row.id)}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
           </button>`
        // Stage 26.9.11: SubcontractOrder is the same "flat doctype, only
        // its state-changing actions need a row button" shape - Send moves
        // raw material out (Draft->Sent), Receive moves the processed/
        // finished good back in (Sent->Received).
        : currentDoctype === 'SubcontractOrder' && row.status === 'Draft'
        ? `<button class="action-btn" title="Send to Subcontractor" aria-label="Send ${escapeHTMLText(row.id)} to subcontractor" style="margin-right:4px;" data-doc-action="send-subcontract-order" data-doc-id="${escapeHTMLText(row.id)}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
           </button>`
        : currentDoctype === 'SubcontractOrder' && row.status === 'Sent'
        ? `<button class="action-btn" title="Receive from Subcontractor" aria-label="Receive ${escapeHTMLText(row.id)} from subcontractor" style="margin-right:4px;" data-doc-action="receive-subcontract-order" data-doc-id="${escapeHTMLText(row.id)}" data-expected-qty="${escapeHTMLText(row.expected_received_qty || '')}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
           </button>`
        // Stage 26.7.9: customer householding/merge - this row (the
        // duplicate) merges INTO another customer id the user provides.
        : currentDoctype === 'Customer' && row.status !== 'Merged'
        ? `<button class="action-btn" title="Merge Into Another Customer" aria-label="Merge ${escapeHTMLText(row.id)} into another customer" style="margin-right:4px;" data-doc-action="merge-customer" data-doc-id="${escapeHTMLText(row.id)}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 3h5v5"/><path d="M8 3H3v5"/><path d="M3 16v5h5"/><path d="M16 21h5v-5"/><line x1="3" y1="3" x2="21" y2="21"/></svg>
           </button>`
        // Stage 26.8.8/26.8.10: Appraisal/Grievance are the same "flat
        // doctype, only the submit-for-approval action is bespoke" shape
        // QualityInspection already uses above.
        : (currentDoctype === 'Appraisal' || currentDoctype === 'Grievance') && row.status === 'Draft'
        ? `<button class="action-btn" title="Submit for Approval" aria-label="Submit ${escapeHTMLText(row.id)} for approval" style="margin-right:4px;" data-doc-action="submit-document" data-doc-id="${escapeHTMLText(row.id)}" data-doctype="${escapeHTMLText(currentDoctype)}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="M12 5l7 7-7 7"/></svg>
           </button>`
        // Stage 36.4.1/36.4.3: run this export template right now and
        // download the CSV - same authenticated-blob pattern as the Product
        // Group Actions modal's Export CSV button, since a plain <a href>
        // can't carry the Bearer token this endpoint requires.
        : currentDoctype === 'PIMExportTemplate' && row.status === 'Active'
        ? `<button class="action-btn" title="Run" aria-label="Run export template ${escapeHTMLText(row.id)}" style="margin-right:4px;" data-doc-action="run-pim-export" data-doc-id="${escapeHTMLText(row.id)}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg>
           </button>`
        // Stage 36.4.4: mint/rotate this catalog's share link and show it
        // once - the same one-time-reveal posture Stage 36.3.4's import
        // hook token and Stage 38.2a's API keys both already take.
        : currentDoctype === 'PIMCatalog'
        ? `<button class="action-btn" title="Share Link" aria-label="Get share link for catalog ${escapeHTMLText(row.id)}" style="margin-right:4px;" data-doc-action="share-pim-catalog" data-doc-id="${escapeHTMLText(row.id)}" data-doc-name="${escapeHTMLText(row.name || row.id)}">
             <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/></svg>
           </button>`
        : '';
      tableHTML += `
        <td class="row-actions-cell" style="text-align: right;">
          ${showHistory ? `<button class="action-btn" title="History" aria-label="History for ${escapeHTMLText(row.id)}" style="margin-right:4px;" data-doc-action="taxonomy-history" data-doc-id="${escapeHTMLText(row.id)}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          </button>` : ''}
          ${prActions}
          ${currentDoctype !== 'PasswordResetRequest' && canUpdateDoctype(currentDoctype) ? `
          <button class="action-btn" title="Edit" aria-label="Edit ${escapeHTMLText(row.id)}" style="margin-right:4px;" data-doc-action="edit" data-doc-id="${escapeHTMLText(row.id)}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
          </button>` : ''}
          ${currentDoctype !== 'PasswordResetRequest' && canDeleteDoctype(currentDoctype) ? `
          <button class="action-btn action-btn-danger" title="Delete" aria-label="Delete ${escapeHTMLText(row.id)}" data-doc-action="delete" data-doc-id="${escapeHTMLText(row.id)}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg>
          </button>` : ''}
        </td>
      </tr>`;
    });
  }

  tableHTML += `</tbody></table>`;
  wrapper.innerHTML = tableHTML;
  const DOC_ACTIONS_IN_PROCUREMENT = new Set(['submit-quality-inspection', 'send-subcontract-order', 'receive-subcontract-order', 'merge-customer', 'submit-document']);
  if (!wrapper.dataset.actionListenersBound) {
    wrapper.dataset.actionListenersBound = 'true';
    wrapper.addEventListener('click', async event => {
      const button = event.target.closest('[data-doc-action]');
      if (!button) return;
      const id = button.dataset.docId;
      // 2026-10-09: these handlers live in view-procurement.js, which is only
      // loaded once someone opens a Procurement screen. Reached first from
      // Manufacturing (Quality Inspections, Subcontracting) or Setup
      // (Customers), the button called an undefined function through ?.()
      // and silently did nothing. Load the module first.
      if (DOC_ACTIONS_IN_PROCUREMENT.has(button.dataset.docAction)) {
        try { await loadViewModule('/view-procurement.js?v=1'); }
        catch (error) { console.error('Could not load the procurement actions', error); }
      }
      switch (button.dataset.docAction) {
        case 'submit-requisition': window.submitRequisitionForApproval?.(id); break;
        case 'convert-requisition': window.convertRequisition?.(id, button.dataset.target); break;
        case 'submit-quality-inspection': window.submitQualityInspectionForApproval?.(id); break;
        case 'send-subcontract-order': window.sendSubcontractOrder?.(id); break;
        case 'receive-subcontract-order': window.receiveSubcontractOrder?.(id, button.dataset.expectedQty); break;
        case 'merge-customer': window.mergeCustomerRow?.(id); break;
        case 'submit-document': window.submitDocForApproval?.(button.dataset.doctype, id); break;
        case 'run-pim-export': window.runPIMExportTemplateRow?.(id); break;
        case 'share-pim-catalog': window.openPIMCatalogShareModal?.(id, button.dataset.docName); break;
        case 'taxonomy-history': window.viewTaxonomyHistory?.(id); break;
        case 'edit': window.editDocRecord?.(id); break;
        case 'delete': window.deleteDocRecord?.(id); break;
      }
    });
    wrapper.addEventListener('change', event => {
      const input = event.target;
      if (input.matches('[data-pim-select-page]')) window.togglePIMBulkPageSelection?.(input.checked);
      else if (input.matches('[data-pim-doc-id]')) window.togglePIMBulkDocSelection?.(input.dataset.pimDocId, input.checked);
    });
  }
  updatePIMBulkEditBar();

  paginator.innerHTML = `
    <span>Showing ${total === 0 ? 0 : start + 1}-${end} of ${total}${docTableLoading ? ' — loading…' : ''}</span>
    <div class="pagination-buttons">
      <button class="pagination-btn" id="doc-table-prev-btn" data-page="${currentTablePage - 1}" ${currentTablePage === 1 || docTableLoading ? 'disabled' : ''}>Previous</button>
      <button class="pagination-btn" id="doc-table-next-btn" data-page="${currentTablePage + 1}" ${currentTablePage === pages || docTableLoading ? 'disabled' : ''}>Next</button>
    </div>
  `;
  if (!paginator.dataset.actionListenersBound) {
    paginator.dataset.actionListenersBound = 'true';
    paginator.addEventListener('click', event => {
      const button = event.target.closest('[data-page]');
      if (button && !button.disabled) window.changeDocPage?.(Number(button.dataset.page));
    });
  }
}

function isPIMBulkEditDoctype() {
  if (currentDoctype === 'Item') return true;
  const active = state.activeDoctypes.find(doc => doc.name === currentDoctype);
  return active && String(active.module || '').toLowerCase() === 'pim';
}

function visibleDocTableItems() {
  // BLD-034: state.docData is already exactly this page's rows (server-side
  // limit/offset/search) - nothing left to re-filter or re-slice locally.
  return state.docData;
}

function updatePIMBulkEditBar() {
  const bar = document.getElementById('pim-bulk-edit-bar');
  const count = document.getElementById('pim-bulk-selection-count');
  const button = document.getElementById('pim-bulk-edit-button');
  if (!bar || !count || !button) return;
  const selected = bulkSelectedDocIDs.size;
  count.textContent = `${selected} selected`;
  button.disabled = selected === 0;
  bar.classList.toggle('hidden', selected === 0);
  const approveBtn = document.getElementById('pim-bulk-approve-button');
  const rejectBtn = document.getElementById('pim-bulk-reject-button');
  if (approveBtn) approveBtn.disabled = selected === 0;
  if (rejectBtn) rejectBtn.disabled = selected === 0;
}

// bulkDecideProductContent (Stage 26.4.6) approves/rejects every currently
// selected ProductContent row in one call - see engines.BulkDecideApproval.
// A rejection needs a comment (APPROV-0159 already enforces this per-
// document server-side); collected once up front rather than per row.
window.bulkDecideProductContent = async function(decision) {
  const ids = [...bulkSelectedDocIDs];
  if (ids.length === 0) return;
  let comment = '';
  if (decision === 'Rejected') {
    comment = (await showCustomPrompt('Reason for rejecting these documents:', '', 'Bulk Reject')) || '';
    if (!comment.trim()) {
      await showCustomAlert('A comment is required to reject.', 'Bulk Reject');
      return;
    }
  }
  if (!await showCustomConfirm(`${decision} ${ids.length} selected content record${ids.length === 1 ? '' : 's'}?`, `Confirm Bulk ${decision}`)) return;
  const res = await apiFetch('/api/v1/approval/bulk-decide', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'ProductContent', document_ids: ids, decision, comment })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, `Bulk ${decision.toLowerCase()} failed.`);
    return;
  }
  const data = await res.json();
  const failedCount = Object.keys(data.failed || {}).length;
  bulkSelectedDocIDs = new Set();
  await showCustomAlert(`${(data.succeeded || []).length} succeeded, ${failedCount} failed.`, `Bulk ${decision} Complete`);
  renderView('doctype-table');
};

window.togglePIMBulkDocSelection = function(id, selected) {
  if (selected) bulkSelectedDocIDs.add(id);
  else bulkSelectedDocIDs.delete(id);
  updatePIMBulkEditBar();
};

window.togglePIMBulkPageSelection = function(selected) {
  visibleDocTableItems().forEach(row => {
    if (selected) bulkSelectedDocIDs.add(row.id);
    else bulkSelectedDocIDs.delete(row.id);
  });
  renderDocTable();
};

// submitRequisitionForApproval (Stage 26.3.2) posts through the same
// generic /api/v1/approval/submit endpoint submitPOForApproval/
// submitExpenseForApproval already use for their own doctypes - Stage 17.7's
// approval_rules for PurchaseRequisition were already configured, this was
// only ever missing a caller.
window.submitRequisitionForApproval = async function(documentId) {
  const res = await apiFetch('/api/v1/approval/submit', {
    method: 'POST',
    body: JSON.stringify({ doctype: 'PurchaseRequisition', document_id: documentId })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to submit for approval.');
    return;
  }
  renderView('doctype-table');
};

// convertRequisition (Stage 26.3.2) is the frontend for
// engines.ConvertRequisitionToOrder (Stage 17.7), which already existed and
// was already routed (POST /api/v1/procurement/convert-requisition) but had
// no UI action calling it. store_code/financial_year aren't stored on the
// requisition itself, so they're asked for here, same as GRN's own workbench
// asks for what its source document doesn't carry.
window.convertRequisition = async function(requisitionId, target) {
  const storeCode = await showCustomPrompt('Store code for the new document:', 'HO', `Convert to ${target === 'RFQ' ? 'RFQ' : 'Purchase Order'}`);
  if (!storeCode) return;
  const financialYear = await showCustomPrompt('Financial year (e.g. 26-27):', '', `Convert to ${target === 'RFQ' ? 'RFQ' : 'Purchase Order'}`);
  if (!financialYear) return;
  const res = await apiFetch('/api/v1/procurement/convert-requisition', {
    method: 'POST',
    body: JSON.stringify({ requisition_id: requisitionId, target, store_code: storeCode, financial_year: financialYear })
  });
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to convert requisition.');
    return;
  }
  const data = await res.json();
  await showCustomAlert(`Converted to ${target === 'RFQ' ? 'RFQ' : 'Purchase Order'} ${data.new_document_id}.`, 'Requisition Converted');
  renderView('doctype-table');
};

// viewTaxonomyHistory (Stage 26.4.3) shows one taxonomy document's existing
// audit_logs trail in a lightweight read-only modal, reusing the same
// .modal-overlay/.modal-container primitives the bulk-edit modal below
// uses instead of introducing a second modal component.
window.viewTaxonomyHistory = async function(id) {
  const res = await apiFetch(`/api/v1/pim/taxonomy-history/${encodeURIComponent(currentDoctype)}/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load history for this record.');
    return;
  }
  const entries = await res.json();

  document.getElementById('pim-history-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-history-modal';
  const rows = entries.length === 0
    ? `<tr><td colspan="3" class="text-center text-muted">No history recorded yet. Entries appear here the first time this record is edited.</td></tr>`
    : entries.map(e => `<tr><td>${e.created_at || ''}</td><td>${e.user_id || ''}</td><td>${e.details || ''}</td></tr>`).join('');
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">History: ${getDoctypeLabel(currentDoctype)} ${id}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <div class="modal-body"><div class="table-wrapper"><table><thead><tr><th>When</th><th>User</th><th>Change</th></tr></thead><tbody>${rows}</tbody></table></div></div>
      <div class="modal-footer"><button type="button" class="btn btn-secondary">Close</button></div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
};

// openPIMProductGroupActionsModal (Stage 36.1.3) is the Product Group's
// production consumer surface: pick a saved static or dynamic group, see how
// many products it resolves to right now, then either export it or bulk edit
// it. It reuses the same .modal-overlay primitives and the same
// /api/v1/pim/bulk-edit endpoint as the selection-based path above - the only
// difference is that the server resolves the target list from the group.
window.openPIMProductGroupActionsModal = async function() {
  const groupsRes = await apiFetch('/api/v1/doc/PIMProductGroup');
  if (!groupsRes) return;
  if (!groupsRes.ok) {
    await showApiError(groupsRes, 'Could not load product groups.');
    return;
  }
  const groups = (await groupsRes.json()).filter(g => (g.status || 'Active') === 'Active');
  if (groups.length === 0) {
    showCustomAlert('No active product groups exist yet. Create one under PIM » Product Group first.', 'Group Actions');
    return;
  }
  const fields = (state.activeDocFields || []).filter(field => field.fieldname !== 'id' && field.fieldname !== 'code');

  document.getElementById('pim-group-actions-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-group-actions-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Product Group Actions</h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label" for="pim-group-select">Product group</label>
          <select class="form-select" id="pim-group-select">${groups.map(g => `<option value="${escapeHTMLText(g.id)}">${escapeHTMLText(g.name || g.id)} (${escapeHTMLText(g.group_type || '')})</option>`).join('')}</select>
        </div>
        <p class="text-muted" style="font-size:13px;" id="pim-group-count">Resolving membership&hellip;</p>
        <div class="form-group"><label class="form-label" for="pim-group-field">Field to edit</label><select class="form-select" id="pim-group-field">${fields.map(f => `<option value="${escapeHTMLText(f.fieldname)}">${escapeHTMLText(getTranslatedLabel(f.label))}</option>`).join('')}</select></div>
        <div class="form-group"><label class="form-label" id="pim-group-value-label">New value</label><div id="pim-group-value"></div></div>
        <p class="text-muted" style="font-size:13px; margin:0;">A dynamic group is re-resolved by the server at the moment you confirm, so the edit applies to whatever matches then - not to the count shown above if the catalog changed in between.</p>

        <div style="margin-top:20px; padding-top:16px; border-top:1px solid var(--border-color);">
          <label class="form-label" style="margin-bottom:8px;" title="Seeds a Draft in the target language, copied from each product's Approved content in the source language - not a machine translation. A human still has to translate the text and submit it for approval like any other content.">Bulk Catalog Translation (Stage 36.7.5)</label>
          <div style="display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap;">
            <div class="form-group" style="margin-bottom:0;">
              <label class="form-label" for="pim-group-xlt-source">Source language</label>
              <input type="text" id="pim-group-xlt-source" class="form-input" style="width:100px;" value="en">
            </div>
            <div class="form-group" style="margin-bottom:0;">
              <label class="form-label" for="pim-group-xlt-target">Target language</label>
              <input type="text" id="pim-group-xlt-target" class="form-input" style="width:100px;" placeholder="e.g. hi">
            </div>
            <button type="button" class="btn btn-outline" id="pim-group-xlt-run">Seed Translations</button>
          </div>
          <div id="pim-group-xlt-result" style="margin-top:12px;"></div>
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" id="pim-group-cancel">Cancel</button>
        <button type="button" class="btn btn-outline" id="pim-group-export">Export CSV</button>
        <button type="button" class="btn btn-primary" id="pim-group-edit">Bulk edit group</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('#pim-group-cancel').addEventListener('click', close);

  const groupSelect = overlay.querySelector('#pim-group-select');
  const countEl = overlay.querySelector('#pim-group-count');
  const refreshCount = async () => {
    countEl.textContent = 'Resolving membership…';
    const res = await apiFetch(`/api/v1/pim/product-groups/${encodeURIComponent(groupSelect.value)}/members`);
    if (!res || !res.ok) { countEl.textContent = 'Could not resolve this group right now.'; return; }
    const resolved = await res.json();
    countEl.textContent = `${resolved.member_count} product${resolved.member_count === 1 ? '' : 's'} currently in this group.`;
  };
  groupSelect.addEventListener('change', refreshCount);

  const fieldSelect = overlay.querySelector('#pim-group-field');
  const renderValueInput = () => {
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const holder = overlay.querySelector('#pim-group-value');
    holder.replaceChildren();
    if (!field) return;
    let input;
    if (field.fieldtype === 'Select') {
      input = document.createElement('select');
      input.className = 'form-select';
      (field.options || '').split(',').filter(Boolean).forEach(value => {
        const option = document.createElement('option');
        option.value = value.trim();
        option.textContent = value.trim();
        input.appendChild(option);
      });
    } else {
      input = document.createElement('input');
      input.className = 'form-input';
      input.type = field.fieldtype === 'Number' ? 'number' : 'text';
    }
    input.id = 'pim-group-value-input';
    holder.appendChild(input);
    overlay.querySelector('#pim-group-value-label').textContent = `New ${getTranslatedLabel(field.label)}`;
  };
  fieldSelect.addEventListener('change', renderValueInput);
  renderValueInput();

  overlay.querySelector('#pim-group-export').addEventListener('click', async () => {
    const res = await apiFetch(`/api/v1/pim/product-groups/${encodeURIComponent(groupSelect.value)}/export.csv`);
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Failed to export this product group.'); return; }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `product_group_${groupSelect.value.replace(/[^A-Za-z0-9_-]/g, '_')}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });

  overlay.querySelector('#pim-group-edit').addEventListener('click', async () => {
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const input = overlay.querySelector('#pim-group-value-input');
    if (!field || !input) return;
    if (field.mandatory && String(input.value).trim() === '') return;
    const value = field.fieldtype === 'Number' ? Number(input.value) : input.value;
    const groupName = groupSelect.options[groupSelect.selectedIndex].textContent;
    if (!await showCustomConfirm(`Update every product currently in ${groupName}?`, 'Confirm Group Bulk Edit')) return;
    const res = await apiFetch('/api/v1/pim/bulk-edit', {
      method: 'POST',
      body: JSON.stringify({ doctype: currentDoctype, group_id: groupSelect.value, field: field.fieldname, value })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Group bulk edit failed. No records were changed.'); return; }
    const result = await res.json();
    close();
    await showCustomAlert(`${result.updated_count} record${result.updated_count === 1 ? '' : 's'} updated.`, 'Bulk Edit Complete');
    renderView('doctype-table');
  });

  overlay.querySelector('#pim-group-xlt-run').addEventListener('click', async () => {
    const source = overlay.querySelector('#pim-group-xlt-source').value.trim();
    const target = overlay.querySelector('#pim-group-xlt-target').value.trim();
    const resultEl = overlay.querySelector('#pim-group-xlt-result');
    if (!source || !target) {
      resultEl.innerHTML = `<div class="login-error">Source and target language are both required.</div>`;
      return;
    }
    const groupName = groupSelect.options[groupSelect.selectedIndex].textContent;
    if (!await showCustomConfirm(`Seed ${target} Draft content (from ${source}) for every product currently in ${groupName}? Existing content in ${target} is left untouched.`, 'Seed Translations')) return;
    resultEl.innerHTML = `<div class="text-muted">Seeding&hellip;</div>`;
    const res = await apiFetch('/api/v1/pim/translations/seed', {
      method: 'POST',
      body: JSON.stringify({ group_id: groupSelect.value, source_language: source, target_language: target })
    });
    if (!res) return;
    if (!res.ok) { await showApiError(res, 'Bulk translation seeding failed.'); resultEl.innerHTML = ''; return; }
    const outcomes = await res.json();
    const ok = outcomes.filter(o => !o.error).length;
    resultEl.innerHTML = `<div class="table-wrapper"><p>${ok} of ${outcomes.length} item(s) seeded.</p><table><thead><tr><th>Item</th><th>Result</th></tr></thead><tbody>${
      outcomes.map(o => `<tr><td style="font-family:monospace;">${escapeHTMLText(o.item_code)}</td><td>${o.error ? `<span class="badge badge-secondary">${escapeHTMLText(o.error)}</span>` : `<span class="badge badge-success">Seeded as Draft</span>`}</td></tr>`).join('')
    }</tbody></table></div>`;
  });

  await refreshCount();
};

// runPIMExportTemplateRow (Stage 36.4.1/36.4.3) runs one export template and
// downloads its CSV - same authenticated-blob pattern as
// downloadReportExportCSV/the search-feed export above.
window.runPIMExportTemplateRow = async function(id) {
  const res = await apiFetch(`/api/v1/pim/export-templates/${encodeURIComponent(id)}/run`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to run this export template.'); return; }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `pim_export_${id.replace(/[^A-Za-z0-9_-]/g, '_')}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
};

// openPIMCatalogShareModal (Stage 36.4.4) mints/rotates a catalog's share
// link and shows the full, ready-to-copy URL exactly once - the raw token
// is never retrievable again after this, the same one-time-reveal posture
// Stage 36.3.4's import hook token and Stage 38.2a's API keys both already
// take. tenant_id travels in the URL itself (not a header) because the link
// has to work from a plain browser click with no session.
window.openPIMCatalogShareModal = async function(id, name) {
  if (!await showCustomConfirm(`Mint a new share link for "${name}"? Any link shared before this will stop working immediately.`, 'Catalog Share Link')) return;
  const res = await apiFetch(`/api/v1/pim/catalogs/${encodeURIComponent(id)}/rotate-share-token`, { method: 'POST' });
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'Failed to mint a share link for this catalog.'); return; }
  const result = await res.json();
  const shareURL = `${location.origin}/pim-catalog-share.html?tenant_id=${encodeURIComponent(result.tenant_id)}&token=${encodeURIComponent(result.share_token)}`;

  document.getElementById('pim-catalog-share-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-catalog-share-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Share Link: ${escapeHTMLText(name)}</h3><button type="button" class="modal-close" aria-label="Close">&times;</button></div>
      <div class="modal-body">
        <p class="text-muted" style="font-size:13px;">Anyone with this link can view this catalog's live product list without signing in. It will not be shown again after you close this window - mint a new link if it is lost.</p>
        <div class="form-group"><input class="form-input" id="pim-catalog-share-url" readonly value="${escapeHTMLText(shareURL)}" ${actionAttrs('selectFieldText', [ACTION_ARG_ELEMENT])}></div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" id="pim-catalog-share-close">Close</button>
        <button type="button" class="btn btn-primary" id="pim-catalog-share-copy">Copy Link</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('#pim-catalog-share-close').addEventListener('click', close);
  overlay.querySelector('#pim-catalog-share-copy').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(shareURL);
      showCustomAlert('Share link copied to clipboard.', 'Copied');
    } catch {
      overlay.querySelector('#pim-catalog-share-url').select();
    }
  });
};

window.openPIMBulkEditModal = function() {
  if (bulkSelectedDocIDs.size === 0) return;
  const fields = state.activeDocFields.filter(field => field.fieldname !== 'id' && field.fieldname !== 'code');
  if (fields.length === 0) {
    showCustomAlert('This document type has no editable fields.', 'Bulk Edit');
    return;
  }

  document.getElementById('pim-bulk-edit-modal')?.remove();
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay open';
  overlay.id = 'pim-bulk-edit-modal';
  overlay.innerHTML = `
    <div class="modal-container">
      <div class="modal-header"><h3 class="modal-title">Edit ${bulkSelectedDocIDs.size} selected ${getDoctypeLabel(currentDoctype)} record${bulkSelectedDocIDs.size === 1 ? '' : 's'}</h3><button type="button" class="modal-close" aria-label="Close">×</button></div>
      <form><div class="modal-body"><div class="form-group"><label class="form-label">Field</label><select class="form-select" id="pim-bulk-field"></select></div><div class="form-group"><label class="form-label" id="pim-bulk-value-label">New value</label><div id="pim-bulk-value"></div></div><p class="text-muted" style="font-size:13px; margin:0;">Preview: this change will update all ${bulkSelectedDocIDs.size} selected records. Approved records are returned to Pending Approval when their doctype is approval-gated.</p></div><div class="modal-footer"><button type="button" class="btn btn-secondary">Cancel</button><button type="submit" class="btn btn-primary">Confirm bulk edit</button></div></form>
    </div>`;
  document.body.appendChild(overlay);

  const close = () => overlay.remove();
  overlay.querySelector('.modal-close').addEventListener('click', close);
  overlay.querySelector('.btn-secondary').addEventListener('click', close);
  const fieldSelect = overlay.querySelector('#pim-bulk-field');
  fields.forEach(field => {
    const option = document.createElement('option');
    option.value = field.fieldname;
    option.textContent = getTranslatedLabel(field.label);
    fieldSelect.appendChild(option);
  });
  const renderValueInput = () => {
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const holder = overlay.querySelector('#pim-bulk-value');
    holder.replaceChildren();
    let input;
    if (field.fieldtype === 'Select') {
      input = document.createElement('select');
      input.className = 'form-select';
      field.options.split(',').filter(Boolean).forEach(value => {
        const option = document.createElement('option');
        option.value = value.trim();
        option.textContent = value.trim();
        input.appendChild(option);
      });
    } else {
      input = document.createElement('input');
      input.className = 'form-input';
      input.type = field.fieldtype === 'Number' ? 'number' : 'text';
    }
    input.id = 'pim-bulk-value-input';
    input.required = field.mandatory;
    holder.appendChild(input);
    overlay.querySelector('#pim-bulk-value-label').textContent = `New ${getTranslatedLabel(field.label)}`;
  };
  fieldSelect.addEventListener('change', renderValueInput);
  renderValueInput();
  overlay.querySelector('form').addEventListener('submit', async event => {
    event.preventDefault();
    const field = fields.find(candidate => candidate.fieldname === fieldSelect.value);
    const input = overlay.querySelector('#pim-bulk-value-input');
    const value = field.fieldtype === 'Number' ? Number(input.value) : input.value;
    if (field.mandatory && String(input.value).trim() === '') return;
    const count = bulkSelectedDocIDs.size;
    if (!await showCustomConfirm(`Update ${count} selected ${getDoctypeLabel(currentDoctype)} record${count === 1 ? '' : 's'}?`, 'Confirm Bulk Edit')) return;
    const res = await apiFetch('/api/v1/pim/bulk-edit', { method: 'POST', body: JSON.stringify({ doctype: currentDoctype, ids: [...bulkSelectedDocIDs], field: field.fieldname, value }) });
    if (!res) return;
    if (!res.ok) {
      await showApiError(res, 'Bulk edit failed. No records were changed.');
      return;
    }
    close();
    bulkSelectedDocIDs = new Set();
    await showCustomAlert(`${count} record${count === 1 ? '' : 's'} updated.`, 'Bulk Edit Complete');
    renderView('doctype-table');
  });
};

window.changeDocPage = function(page) {
  currentTablePage = page;
  refreshDocTablePage();
  saveNavState();
};

window.deleteDocRecord = async function(id) {
  if (await showCustomConfirm('Delete this record?')) {
    // BLD-035: unencoded, this 404s for the great majority of real records -
    // Stage 51.1 made every Master/Transaction's id equal its human-readable
    // code (e.g. "Vendor/HQ/2026/000002"), and an un-encoded "/" splits the
    // URL into extra path segments the {id} route pattern doesn't match.
    // savePurchaseOrder's own PO composer already encodes its equivalent URL
    // - this generic path just never got the same fix.
    const res = await apiFetch(`/api/v1/doc/${currentDoctype}/${encodeURIComponent(id)}`, { method: 'DELETE' });
    if (!res) return;
    if (res.ok) {
      renderView('doctype-table');
    } else {
      await showApiError(res, 'Failed to delete record.');
    }
  }
};

// Stage 50/AUD-09: after a successful save (or Cancel, or the X button),
// document.activeElement was left on the dialog's own Save/Cancel/close
// button, now hidden by modal.classList.remove('open') - a keyboard or
// screen-reader user's next Tab/interaction went nowhere visible, since
// focus was sitting inside a display:none subtree. These two helpers are a
// shared choke point rather than a per-modal fix: capture whatever had
// focus right before a modal opened (almost always the button that
// triggered it - "+ New Vendor", a row's "Edit" action, etc.), and restore
// focus there when it closes. If that element is gone - the common case
// BLD-011 calls out explicitly: the record's own row/trigger was removed
// (deleted, or the list re-rendered) - fall back to #view-root, given
// `tabindex="-1"` in index.html for exactly this, rather than leaving focus
// on a now-invisible element or letting it silently fall back to <body>.
let modalReturnFocusEl = null;
function captureFocusForModalReturn() {
  modalReturnFocusEl = document.activeElement;
}
function restoreFocusAfterModalClose() {
  const target = modalReturnFocusEl;
  modalReturnFocusEl = null;
  if (target && document.body.contains(target) && typeof target.focus === 'function') {
    target.focus();
    return;
  }
  document.getElementById('view-root')?.focus();
}

// Stage 50/BLD-011 follow-up: live-verifying AUD-09's Done bar ("cover ...
// save/cancel/Escape") found Escape did nothing at all here - Save and
// Cancel both already ran through closeDynamicModal() (the modal's own
// buttons), but there was no keyboard equivalent, unlike this app's other
// Escape-closable surfaces (nav drawer, account menu, submenus). One
// document-level handler covers it without touching the modal's own markup.
document.addEventListener('keydown', (e) => {
  const modal = document.getElementById('dynamic-modal');
  if (!modal?.classList.contains('open')) return;
  // A confirmation/error dialog above this form owns its own keyboard input.
  if (!document.getElementById('custom-dialog-container')?.classList.contains('hidden')) return;
  if (e.key === 'Escape') {
    e.preventDefault();
    closeDynamicModal();
  } else if (e.key === 'Tab') {
    const controls = [...modal.querySelectorAll('button, input:not([type="hidden"]), select, textarea, a[href], [tabindex]')]
      .filter(el => !el.disabled && el.tabIndex >= 0 && el.getClientRects().length && !el.closest('[inert]'));
    const first = controls[0], last = controls[controls.length - 1];
    const focus = document.activeElement;
    if (first && (!modal.contains(focus) || (e.shiftKey ? focus === first : focus === last))) {
      e.preventDefault();
      (e.shiftKey ? last : first).focus();
    }
  }
});

// Open Dynamic Creation Modal. Pass an existing record (as returned by
// GET /api/v1/doc/{doctype}/{id}) to switch into edit mode instead of
// create - see editDocRecord below, the only caller that does this.
window.openDynamicModal = async function(existingRecord) {
  // These are audit records emitted by the dedicated reset workflow. Users
  // are identities, not documents: generic Link(User)/CRUD cannot create a
  // valid reset and must not replace the audited dual-control endpoint.
  if (currentDoctype === 'PasswordResetRequest') {
    await renderView('users');
    return;
  }
  captureFocusForModalReturn();
  const modal = document.getElementById('dynamic-modal');
  const title = document.getElementById('dynamic-modal-title');
  const body = document.getElementById('dynamic-modal-body');
  if (!modal) return;

  // Only a fetched record selects edit mode, never a DOM click event.
  const isEdit = !!existingRecord && typeof existingRecord.id === 'string' && existingRecord.id.length > 0;
  editingDocID = isEdit ? existingRecord.id : null;
  editingDocVersion = isEdit && typeof existingRecord.version === 'number' ? existingRecord.version : null;
  title.textContent = `${isEdit ? 'Edit' : 'New'} ${getDoctypeLabel(currentDoctype)}`;
  body.innerHTML = '';

  const activeDoc = state.activeDoctypes.find(d => d.name === currentDoctype);
  const isMaster = activeDoc && activeDoc.document_type === 'Master';
  const isPurchaseRequisition = currentDoctype === 'PurchaseRequisition';

  for (const f of state.activeDocFields) {
    if (f.fieldname === 'id') continue;
    // Stage 30.5.6: f.mirrored marks the derived half of a duplicate
    // mandatory pair (PurchaseOrder's vendor_id against vendor). The server
    // copies it across at the same choke point that numbers the document, so
    // asking for it here would be asking the user to type the same value into
    // a second identically-named required box. The flag is computed from the
    // registry that does the copying, not from a doctype list kept here.
    if (f.mirrored) continue;

    // f.auto_generated (Stage 30.6) is stamped on by the server for the
    // document-number fields it issues itself, so this form stops asking for
    // a value it would discard. It comes from the same registry that assigns
    // the number (engines/document_numbering.go), rather than a copy of the
    // doctype list kept here.
    const isCodeField = f.auto_generated || ((isMaster || isPurchaseRequisition) && f.fieldname.toLowerCase() === 'code');
    const existingVal = isEdit ? existingRecord[f.fieldname] : undefined;

    const fg = document.createElement('div');
    fg.className = 'form-group';
    // Stage 50/AUD-08: every field this builder renders shared one sibling
    // <label> with no `for` and no matching control `id` - a screen reader
    // announced ten fields on the Vendor dialog alone as unnamed controls,
    // reachable only by guessing from surrounding visual layout. doctype+
    // fieldname is already the unique key this form's own submit handler
    // reads fields back by ([name="<fieldname>"]), so it is also a safe,
    // stable id here - one association point for every field type below,
    // rather than a per-branch fix that a new fieldtype could miss.
    const fieldId = `dyn-field-${currentDoctype}-${f.fieldname}`;
    fg.innerHTML = `<label class="form-label" for="${fieldId}">${getTranslatedLabel(f.label)}${f.mandatory && !isCodeField ? '<span class="required">*</span>' : ''}</label>`;

    if (f.fieldtype === 'Select') {
      const select = document.createElement('select');
      select.className = 'form-select';
      select.id = fieldId;
      select.name = f.fieldname;
      select.required = f.mandatory;
      select.innerHTML = '<option value="" disabled selected>— Select Option —</option>';
      const opts = f.options.split(',');
      opts.forEach(o => {
        select.innerHTML += `<option value="${o.trim()}">${o.trim()}</option>`;
      });
      if (existingVal !== undefined && existingVal !== null) select.value = existingVal;
      // Stage 57.4: a new record's Status starts on Active (defaultSelectValue).
      else if (!isEdit) {
        const preset = defaultSelectValue(f, opts.map(o => o.trim()));
        if (preset) select.value = preset;
      }
      fg.appendChild(select);
    } else if (f.fieldtype === 'Link') {
      const select = document.createElement('select');
      select.className = 'form-select';
      select.id = fieldId;
      select.name = f.fieldname;
      select.required = f.mandatory;
      select.dataset.linkDoctype = f.options;
      select.innerHTML = '<option value="" disabled selected>— Loading Lookups —</option>';
      fg.appendChild(select);

      // Stage 57.2: the last option creates a new record inline, fills this
      // select with it and leaves the form open - instead of the user having
      // to abandon the form to go and set the master up.
      const CREATE_OPTION = '__quick_create__';
      const appendCreateOption = () => {
        if (!canCreateDoctype(f.options) || select.querySelector(`option[value="${CREATE_OPTION}"]`)) return;
        const opt = document.createElement('option');
        opt.value = CREATE_OPTION;
        opt.textContent = `+ Create new ${getDoctypeLabel(f.options)}…`;
        select.appendChild(opt);
      };
      let lastLinkValue = '';
      select.addEventListener('focus', () => { lastLinkValue = select.value === CREATE_OPTION ? '' : select.value; });
      select.addEventListener('change', async () => {
        if (select.value !== CREATE_OPTION) { lastLinkValue = select.value; return; }
        select.value = lastLinkValue;
        const doc = await openQuickCreate(f.options, '');
        if (!doc) return;
        const id = doc.id == null ? '' : String(doc.id);
        if (![...select.options].some(o => o.value === id)) {
          const opt = document.createElement('option');
          opt.value = id;
          opt.textContent = doc.name || doc.code || id;
          select.insertBefore(opt, select.querySelector(`option[value="${CREATE_OPTION}"]`));
        }
        select.value = id;
        lastLinkValue = id;
        fg.querySelector('.empty-state-hint')?.remove();
        select.dispatchEvent(new Event('change', { bubbles: true }));
      });

      // Fetch target link options asynchronously
      apiFetch(`/api/v1/doc/${f.options}`).then(res => {
        if (!res || !res.ok) {
          select.innerHTML = '<option value="" disabled selected>— Failed to load options —</option>';
          return;
        }
        return res.json().then(data => {
          // Stage 30.5.1: an empty target list used to render as a dropdown
          // containing only "— Select Reference —" with no indication that
          // anything was wrong, let alone what to do about it. This is the
          // single choke point for every Link field on every generic form,
          // so attaching the affordance here covers all of them at once
          // (10 of the 18 core master lists were empty at audit time).
          if (!data || data.length === 0) {
            select.innerHTML = `<option value="" disabled selected>— No ${getDoctypeLabel(f.options)} records yet —</option>`;
            appendCreateOption();
            // With inline create on offer the select itself is the way out;
            // the navigate-away hint stays only for a user who cannot create.
            if (!canCreateDoctype(f.options)) fg.insertAdjacentHTML('beforeend', emptyPickerHint(f.options));
            return;
          }
          select.innerHTML = '<option value="" disabled selected>— Select Reference —</option>';
          data.forEach(item => {
            // Value must be item.id - that's what the backend's Link
            // existence check (engines.ValidateDocument) actually verifies
            // against. Using item.name here (pre-18.2 fix) stored the wrong
            // value for any target doctype whose `name` differs from its
            // `id`/`code` (Vendor, Customer, Location, Item all qualify),
            // silently breaking the Link constraint it was meant to enforce.
            const option = document.createElement('option');
            option.value = item.id == null ? '' : String(item.id);
            option.textContent = item.name || item.label || item.title || item.code || item.id || '';
            select.appendChild(option);
          });
          if (existingVal !== undefined && existingVal !== null) select.value = existingVal;
          // Stage 57.2: a value carried across a setup detour, applied now the
          // options it names exist (restoreFormValues).
          else if (select.dataset.pendingValue && [...select.options].some(o => o.value === select.dataset.pendingValue)) select.value = select.dataset.pendingValue;
          appendCreateOption();
        });
      });
    } else if (f.fieldtype === 'JSONTable' || f.fieldtype === 'JSONMap') {
      // Stage 30.5.3. The editor writes its serialised value into a hidden
      // input carrying the field's own name, so handleDynamicFormSubmit -
      // which reads `[name="<fieldname>"]`.value - needed no change at all,
      // and the stored representation is byte-identical to what a user used
      // to hand-type. Every Go consumer keeps reading exactly what it read.
      // Stage 33: the form is a multi-column grid now, and this editor is a
      // table - it takes the whole row rather than a 240px column.
      fg.classList.add('form-group-wide');
      renderJSONLineEditor(fg, f, existingVal);
    } else if (f.fieldtype === 'Number') {
      const input = document.createElement('input');
      input.className = 'form-input';
      input.id = fieldId;
      input.type = 'number';
      input.name = f.fieldname;
      input.required = f.mandatory;
      if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      fg.appendChild(input);
    } else if (f.fieldtype === 'Currency') {
      // BLD-035: 'Currency' fell into the plain-text else-branch below with no
      // numeric constraint at all - a mandatory money field accepted arbitrary
      // text. step=0.01 matches this codebase's money precision everywhere
      // else (paise-rounded via formatMoney's en-IN 2-decimal grouping);
      // handleDynamicFormSubmit parses this the same way as 'Number'.
      const input = document.createElement('input');
      input.className = 'form-input';
      input.id = fieldId;
      input.type = 'number';
      input.step = '0.01';
      input.name = f.fieldname;
      input.required = f.mandatory;
      if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      fg.appendChild(input);
    } else if (f.fieldtype === 'Date') {
      // BLD-035: 'Date' also fell into the plain-text else-branch - every one
      // of this doctype's Date fields (Attendance, Leave, Asset, ExpenseClaim,
      // RFQ target_date, ...) rendered as a bare text box with no date picker
      // at all, despite ~15 other hand-built screens elsewhere in this app
      // already using a native date input for the same kind of field. Matches
      // that existing convention rather than inventing a new one.
      const input = document.createElement('input');
      input.className = 'form-input';
      input.id = fieldId;
      input.type = 'date';
      input.name = f.fieldname;
      input.required = f.mandatory;
      if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      fg.appendChild(input);
    } else {
      const input = document.createElement('input');
      input.className = 'form-input';
      input.id = fieldId;
      input.type = 'text';
      input.name = f.fieldname;
      if (isCodeField) {
        // On edit the code already exists and must not be regenerated -
        // show it read-only same as the create-mode placeholder behavior,
        // just with the real value instead of "auto-generated" text.
        input.readOnly = true;
        input.required = false;
        if (isEdit) {
          input.value = existingVal ?? '';
        } else if (f.auto_generated || isPurchaseRequisition) {
          input.placeholder = 'Auto-generated from Prefix Configs on save';
        } else {
          input.placeholder = 'Auto-generated upon save';
        }
      } else if (isDerivedCompanionField(f.fieldname)) {
        // Stage 41: a derived companion ("phone_country") is written by the
        // server from another field's value - the phone engine resolves it
        // from the number itself. Showing it as an empty box invites a user to
        // type something that will be overwritten on the very next save, so it
        // is presented the same way an auto-generated code is: visible,
        // read-only, and labelled with where its value comes from.
        input.readOnly = true;
        input.required = false;
        input.value = existingVal ?? '';
        if (!input.value) input.placeholder = 'Set automatically on save';
      } else {
        input.required = f.mandatory;
        if (existingVal !== undefined && existingVal !== null) input.value = existingVal;
      }
      // Stage 36.7.4: a "Generate" button beside Item.barcode - the one
      // field this ERP mints a check-digit-correct EAN-13 for on request.
      // Scoped to this exact field rather than every text input, since no
      // other field has a generator behind it.
      if (currentDoctype === 'Item' && f.fieldname === 'barcode' && !isCodeField) {
        // Stage 57.7: optional - issued by the barcode policy when left blank.
        if (!input.value) input.placeholder = 'Optional - leave blank to have one issued';
        const row = document.createElement('div');
        row.style.display = 'flex';
        row.style.gap = '8px';
        input.style.flex = '1';
        const genBtn = document.createElement('button');
        genBtn.type = 'button';
        genBtn.className = 'btn btn-outline';
        genBtn.textContent = 'Generate';
        genBtn.title = 'Generate a check-digit-correct EAN-13 barcode';
        genBtn.addEventListener('click', async () => {
          genBtn.disabled = true;
          const res = await apiFetch('/api/v1/pim/barcode/generate', { method: 'POST' });
          genBtn.disabled = false;
          if (!res) return;
          if (!res.ok) { await showApiError(res, 'Failed to generate a barcode.'); return; }
          const result = await res.json();
          input.value = result.barcode;
        });
        row.appendChild(input);
        row.appendChild(genBtn);
        fg.appendChild(row);
      } else {
        fg.appendChild(input);
      }
    }
    body.appendChild(fg);
  }

  // The requirement catalogue is deliberately a soft suggestion rather than
  // a restrictive Link field: new wording is valid, and the server learns it
  // into PurchaseRequisitionDescription on save. Department is an existing
  // Core master and uses the same picker, returning its code for the stored
  // requisition value.
  if (isPurchaseRequisition) {
    const descriptionInput = body.querySelector('[name="description"]');
    const departmentInput = body.querySelector('[name="department"]');
    if (descriptionInput) {
      attachLinkTypeahead(descriptionInput, 'PurchaseRequisitionDescription', {
        valueFields: ['description'],
        labelFn: doc => doc.description || doc.code || doc.id,
        // Not something a user "sets up": the server learns each new wording
        // into this catalogue on save, so a hint telling them to go create one
        // would be advising them to do by hand what already happens by itself.
        noSetupHint: true
      });
    }
    if (departmentInput) attachLinkTypeahead(departmentInput, 'Department');
  }

  // Stage 57.3: "HSN Code* - ask to create the master in New Item". The HSN
  // box becomes a picker over the HSN catalogue with inline create, and a
  // chosen code's default GST rate fills an empty rate field.
  if (currentDoctype === 'Item') {
    attachHSNPicker(body.querySelector('[name="hsn_code"]'), body.querySelector('[name="gst_rate"]'));
  }

  // Stage 55.7: a Bin's zone must be an existing Zone's code, which is
  // auto-numbered - so it is picked (showing the zone's name), and a zone
  // that does not exist yet is created right here from the picker.
  if (currentDoctype === 'Bin') {
    const zoneInput = body.querySelector('[name="zone"]');
    if (zoneInput && zoneInput.tagName === 'INPUT') attachLinkTypeahead(zoneInput, 'Zone', { showAllOnFocus: true });
  }

  // Stage 57.10: an Offer's Scope Value follows its Scope.
  if (currentDoctype === 'Offer') wireOfferScopeValue(body);

  // Stage 33: only the long forms get the wide, multi-column dialog. Item
  // renders ~20 fields and was taller than any screen in one column; a
  // 3-field master in the same 920px box would just be empty space. The
  // column count itself is the stylesheet's job (an intrinsic grid), so
  // this is the one thing that genuinely needs the field count.
  const container = modal.querySelector('.modal-container');
  if (container) {
    container.classList.toggle('modal-container-wide', body.querySelectorAll('.form-group').length > 4);
  }

  // Stage 41: this form is built into the modal, not into #view-root, so
  // renderView's sweep never sees it. One call here covers every generic
  // record form in the product - which is where most phone fields actually
  // live (Customer, Vendor, Employee, Location).
  applyPhoneRulesIn(body);

  modal.inert = false;
  modal.classList.add('open');
  const firstInput = body.querySelector('input:not([type="hidden"]):not([readonly]):not([disabled]), select:not([disabled]), textarea:not([disabled])');
  (firstInput || modal.querySelector('.modal-close'))?.focus();
};

// Stage 57.10 (user decision 2026-10-07): offers target a single Item, a
// Category, a PIM Product Group or a list of SKUs. scope_value used to be one
// bare text box whatever the scope, so the user had to know and type exact
// codes ("its preety bad"). The box is rebuilt for the chosen scope - a fresh
// element each time, so no picker's listeners outlive the scope they were for
// - and keeps its name/id, so the form's own save reads it unchanged.
let offerCategoryList = null;
function wireOfferScopeValue(body) {
  const scopeEl = body.querySelector('[name="scope"]');
  let valueEl = body.querySelector('[name="scope_value"]');
  if (!scopeEl || !valueEl) return;
  const hint = document.createElement('div');
  hint.style.cssText = 'font-size: 12px; color: var(--text-muted); margin-top: 4px;';
  valueEl.insertAdjacentElement('afterend', hint);

  const rebuild = async (keepValue) => {
    const scope = scopeEl.value;
    const isList = scope === 'SKU List';
    const fresh = document.createElement(isList ? 'textarea' : 'input');
    if (!isList) fresh.type = 'text';
    fresh.className = 'form-input';
    fresh.id = valueEl.id;
    fresh.name = valueEl.name;
    fresh.autocomplete = 'off';
    if (isList) fresh.rows = 3;
    fresh.value = keepValue ? valueEl.value : '';
    valueEl.replaceWith(fresh);
    valueEl = fresh;
    hint.textContent = '';

    if (scope === 'Item') {
      fresh.placeholder = 'Search the item';
      attachLinkTypeahead(fresh, 'Item');
    } else if (scope === 'Product Group') {
      fresh.placeholder = 'Choose a PIM product group';
      attachLinkTypeahead(fresh, 'PIMProductGroup', { showAllOnFocus: true });
      hint.textContent = 'Every product in the group gets the offer - a dynamic group follows its rules as products change.';
    } else if (scope === 'SKU List') {
      fresh.placeholder = 'SKU codes, separated by commas or one per line';
      hint.textContent = 'Only these SKUs get the offer.';
    } else if (scope === 'Category') {
      fresh.placeholder = 'Category, as set on the items';
      // The categories already in use, offered as suggestions. Item.category
      // is free text, so there is no master to pick from; reading the items
      // once per session is enough to stop typos.
      if (!offerCategoryList) {
        offerCategoryList = [];
        const res = await apiFetch('/api/v1/doc/Item?limit=1000');
        if (res && res.ok) {
          const seen = new Set();
          for (const it of await res.json()) {
            const c = String(it.category || '').trim();
            if (c && !seen.has(c.toLowerCase())) { seen.add(c.toLowerCase()); offerCategoryList.push(c); }
          }
          offerCategoryList.sort((a, b) => a.localeCompare(b));
        }
      }
      let list = document.getElementById('offer-category-options');
      if (!list) {
        list = document.createElement('datalist');
        list.id = 'offer-category-options';
        document.body.appendChild(list);
      }
      list.replaceChildren(...offerCategoryList.map(c => Object.assign(document.createElement('option'), { value: c })));
      if (valueEl === fresh) fresh.setAttribute('list', list.id);
    } else {
      fresh.readOnly = true;
      fresh.placeholder = 'Not used - the offer applies to the whole bill';
    }
  };
  rebuild(true);
  scopeEl.addEventListener('change', () => rebuild(false));
}

// 21.9 QA-follow-up: the generic record-list screens (Vendors,
// Bin Master, everything under Master Definition, etc.) had a Delete
// action but no way to correct a mistake short of delete-and-recreate -
// a real gap USER_GUIDE.md's own §8 claimed didn't exist. Reuses the
// exact same modal/fields/submit path as create, just pre-filled and
// posted to the /{id} update route the generic doc engine already serves.
window.editDocRecord = async function(id) {
  // BLD-035: see deleteDocRecord's own note just above - same unencoded-slash
  // 404, this time on loading the record to edit rather than deleting it.
  const res = await apiFetch(`/api/v1/doc/${currentDoctype}/${encodeURIComponent(id)}`);
  if (!res) return;
  if (!res.ok) {
    await showApiError(res, 'Failed to load record for editing.');
    return;
  }
  const record = await res.json();
  editingDocID = id;
  editingDocVersion = typeof record.version === 'number' ? record.version : null;
  await openDynamicModal(record);
};

window.closeDynamicModal = function() {
  const modal = document.getElementById('dynamic-modal');
  if (modal) {
    modal.classList.remove('open');
    modal.inert = true;
    document.getElementById('dynamic-modal-form').reset();
  }
  editingDocID = null;
  editingDocVersion = null;
  restoreFocusAfterModalClose();
};

window.handleDynamicFormSubmit = async function(e) {
  e.preventDefault();
  const form = document.getElementById('dynamic-modal-form');
  // BLD-036: guards the whole save (including the server round trip below)
  // against a double-click/repeat-Enter creating two documents - see
  // guardAgainstDoubleSubmit's own comment.
  const submitBtn = form.querySelector('.modal-footer button[type="submit"]');
  await guardAgainstDoubleSubmit(submitBtn, 'Saving...', () => handleDynamicFormSubmitInner(form));
};

async function handleDynamicFormSubmitInner(form) {
  const payload = {};

  const activeDoc = state.activeDoctypes.find(d => d.name === currentDoctype);
  const isMaster = activeDoc && activeDoc.document_type === 'Master';
  const isPurchaseRequisition = currentDoctype === 'PurchaseRequisition';
  // Stage 51.5: an Item linked to a parent design (family) gets its
  // Combination ID/SKU generated server-side from that design plus its
  // variant attributes (engines.PrepareItemVariantCode) - the client must
  // not pre-fill code via the admin-only /api/v1/sequence endpoint below in
  // that case, or the server would never see an empty code to generate one
  // for. A standalone Item (no family selected) is unaffected and keeps its
  // ordinary sequence-numbered code.
  const itemFamilyValue = currentDoctype === 'Item' ? (form.querySelector('[name="family"]')?.value || '').trim() : '';
  let codeFieldname = null;

  state.activeDocFields.forEach(f => {
    if (f.fieldname === 'id') return;
    // Mirrored fields (30.5.6) have no input on the form - the server fills
    // them from their primary. Sending an empty string would overwrite the
    // copy it just made.
    if (f.mirrored) return;
    // Stage 30.6: f.auto_generated joins PurchaseRequisition as a field the
    // server numbers during the save itself, so it must not be routed to the
    // admin-only /api/v1/sequence endpoint below (a Store Manager creating a
    // PO would get a 403 from it) and must not be sent at all - a supplied id
    // is treated as an upsert, which would turn a create into an overwrite.
    const isServerNumbered = f.auto_generated || (isPurchaseRequisition && f.fieldname.toLowerCase() === 'code') ||
      (currentDoctype === 'Item' && f.fieldname.toLowerCase() === 'code' && itemFamilyValue !== '');
    const isCodeField = isServerNumbered || (isMaster && f.fieldname.toLowerCase() === 'code');
    const input = form.querySelector(`[name="${f.fieldname}"]`);
    if (input) {
      if (isCodeField && !input.value) {
        // Master records retain the existing admin sequence endpoint behavior.
        if (!isServerNumbered) codeFieldname = f.fieldname;
      } else {
        if (f.fieldtype === 'Number' || f.fieldtype === 'Currency') {
          payload[f.fieldname] = parseFloat(input.value);
        } else {
          payload[f.fieldname] = input.value;
        }
      }
    }
  });

  if (codeFieldname) {
    const seqRes = await apiFetch('/api/v1/sequence', {
      method: 'POST',
      body: JSON.stringify({
        doc_type: currentDoctype,
        store_code: 'HQ',
        financial_year: new Date().getFullYear().toString()
      })
    });
    if (seqRes && seqRes.ok) {
      const seqData = await seqRes.json();
      payload[codeFieldname] = seqData.code;
    } else {
      await showApiError(seqRes, 'Failed to generate Code sequence.');
      return;
    }
  }

  const isEdit = !!editingDocID;
  if (isEdit && editingDocVersion !== null) {
    payload.expected_version = editingDocVersion;
  }
  // BLD-035: see deleteDocRecord's own note above - same unencoded-slash
  // 404, this time on the save itself. This is the one of the three that
  // was actually live and reachable (edit-load/delete's own copies of this
  // bug were found alongside it, same root cause).
  const endpoint = isEdit ? `/api/v1/doc/${currentDoctype}/${encodeURIComponent(editingDocID)}` : `/api/v1/doc/${currentDoctype}`;
  const res = await apiFetch(endpoint, {
    method: 'POST',
    body: JSON.stringify(payload)
  });

  if (res && res.ok) {
    // BLD-035: the generic doc engine now reports the document's real
    // resulting status here (previously always the literal "saved") - surface
    // it instead of closing silently, so an edit that reset an Approved
    // record back to Pending Approval isn't invisible to the person who just
    // made it. 'warning' for a not-yet-final outcome, 'success' otherwise.
    const savedData = await res.json().catch(() => null);
    const resultStatus = savedData && savedData.status;
    showToast(describeDocumentStatusOutcome(getDoctypeLabel(currentDoctype), resultStatus), {
      variant: resultStatus === 'Pending Approval' ? 'warning' : 'success'
    });
    closeDynamicModal();
    // Stage 41: the record that was just created may be the first of its
    // record type, which means every "No Vendors have been set up yet" hint
    // in the app is now wrong. Refreshed before the re-render so the screen
    // the user lands on is already correct. Awaited rather than fired off,
    // because the render below reads the result.
    await refreshSetupStatus();
    // 2026-09-22: a create reached via the "create the missing master"
    // shortcut (openSetupDoctype, e.g. GRN's Item picker) returns straight
    // to the screen the shortcut was launched from, instead of always
    // landing on the new master's own list - see quickCreateReturn's
    // declaration. Scoped to create (not edit) - editing an unrelated
    // existing record from a shortcut-opened list is just browsing, not
    // the flow the shortcut exists for.
    const returnTo = (!isEdit && quickCreateReturn && quickCreateReturn.forDoctype === currentDoctype) ? quickCreateReturn : null;
    // Stage 57.2: the shared return restores the origin list, reopens the
    // form that was open there with what had been typed, and fills in the
    // record just created - and unwinds the detour's history entry.
    if (returnTo) returnFromQuickCreate(savedData && savedData.id);
    else { quickCreateReturn = null; renderView('doctype-table'); }
    // Stage 50/AUD-09 follow-up: closeDynamicModal() just above correctly
    // restored focus to the "+ New"/row "Edit" button that opened this modal
    // (captureFocusForModalReturn/restoreFocusAfterModalClose) - but
    // renderView('doctype-table') then rebuilds the whole list, which
    // REMOVES that exact button from the document and replaces it with a
    // fresh one. Removing the currently-focused element resets focus to
    // <body> (browser default, not this app's choice), so a keyboard or
    // screen-reader user's next Tab/interaction silently landed at the very
    // top of the document instead of anywhere near the record they just
    // saved - found live-verifying AUD-09 with Playwright (activeElement was
    // BODY after every successful save, never the button or #view-root).
    // Land on the same #view-root fallback restoreFocusAfterModalClose()
    // already uses when its own target is gone, since the just-rebuilt
    // button is exactly that case one render tick later.
    document.getElementById('view-root')?.focus();
  } else if (res) {
    await showApiError(res, isEdit ? 'Failed to save changes - someone else may have edited this record, refresh and try again.' : 'Failed to save record.');
  }
}

// Render Database Schema Design UI (internal name still DocType Builder -
// see docs/micro_checklist.md Stage 2.1 for the historical build record)
async function renderDocTypeBuilderView(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Database Schema Design</h1>
      <p class="page-subtitle">Configure schema structures, define dynamic fields, and setup RBAC rules.</p>
    </div>
    <button class="btn btn-primary" ${actionAttrs('openNewDoctypeModal')}>
      <span>Register New Record Type</span>
    </button>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.display = 'grid';
  panel.style.gridTemplateColumns = '250px 1fr';
  panel.style.gap = '24px';
  panel.style.padding = '24px';

  // Module-only list, each module's own record types revealed in a hover
  // flyout - reuses the sidebar's own .has-flyout/.menu-flyout mechanism
  // (setupModuleFlyouts()/openFlyout()/closeSubmenus()) rather than a
  // second one, per explicit user request to feel identical to the sidebar
  // ("I will hover and select"). Every doctype already carries a `module`
  // (set via openNewDoctypeModal's "Module Group" prompt below).
  const doctypesByModule = {};
  state.activeDoctypes.forEach(d => {
    const mod = d.module || 'Other';
    (doctypesByModule[mod] = doctypesByModule[mod] || []).push(d);
  });

  let listHTML = `<ul class="doctype-module-list" style="border-right: 1px solid var(--border-color); padding-right: 16px; list-style: none;">`;
  Object.keys(doctypesByModule).sort().forEach(mod => {
    listHTML += `
      <li class="menu-item-container has-flyout">
        <a class="menu-item menu-item-group" href="#">
          <span>${mod}</span>
          <svg class="menu-item-arrow flyout-arrow" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="9 6 15 12 9 18"></polyline>
          </svg>
        </a>
        <ul class="menu-flyout">
          ${doctypesByModule[mod].map(d => `<li><a class="menu-item" ${actionAttrs('loadDoctypeConfig', [d.name])}><span>${d.name} (${d.document_type})</span></a></li>`).join('')}
        </ul>
      </li>
    `;
  });
  listHTML += `</ul><div id="doctype-fields-config">Hover a module on the left, then select a record type to configure its metadata schema properties.</div>`;
  panel.innerHTML = listHTML;
  container.appendChild(panel);
  setupModuleFlyouts();
}

window.openNewDoctypeModal = async function() {
  const name = await showCustomPrompt('Enter Record Type Name:');
  if (!name) return;
  const module = await showCustomPrompt('Enter Module Group (e.g. Master Data, Procurement):');
  if (!module) return;
  const docType = await showCustomPrompt('Document Type (Master/Transaction):');
  if (!docType) return;

  const res = await apiFetch('/api/v1/meta/doctypes', {
    method: 'POST',
    body: JSON.stringify({ name, module, document_type: docType })
  });
  if (!res) return;
  if (res.ok) {
    await fetchRegisteredDoctypes();
    renderView('doctype-builder');
  } else {
    await showApiError(res, 'Failed to register record type.');
  }
};

window.loadDoctypeConfig = async function(doctypeName, liveContainer = null) {
  const container = liveContainer?.isConnected ? liveContainer : document.getElementById('doctype-fields-config');
  if (!container) return;

  const res = await apiFetch(`/api/v1/doc/${doctypeName}/meta`);
  if (!res) return;
  if (!res.ok) {
    const msg = await getErrorMessage(res, `Failed to load fields for ${doctypeName}.`);
    renderErrorPanel(container, msg, retryContainer => loadDoctypeConfig(doctypeName, retryContainer));
    return;
  }
  const fields = await res.json();

  let html = `
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 16px;">
      <h3 style="font-size: 18px; font-weight:600;">Fields for ${doctypeName}</h3>
      <button class="btn btn-outline btn-sm" ${actionAttrs('addNewFieldConfig', [doctypeName])}>Add Field</button>
    </div>
    <table>
      <thead>
        <tr>
          <th>Fieldname</th>
          <th>Label</th>
          <th>Fieldtype</th>
          <th>Mandatory</th>
          <th>Options</th>
          <th>Order</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
  `;

  // Stage 41: the row's fields are stashed on the element rather than
  // re-fetched or serialised into the onclick attribute - `options` is free
  // text that can contain quotes, which an inline attribute would break on.
  const rowsHTML = fields.map((f, i) => `
      <tr>
        <td style="font-family: monospace;">${escapeHTMLText(f.fieldname)}</td>
        <td>${escapeHTMLText(f.label)}</td>
        <td>${escapeHTMLText(f.fieldtype)}</td>
        <td>${f.mandatory ? 'Yes' : 'No'}</td>
        <td>${escapeHTMLText(f.options || '—')}</td>
        <td>${f.display_order}</td>
        <td>
          <button class="action-btn" ${actionAttrs('editFieldConfig', [doctypeName, i])}>Edit</button>
          <button class="action-btn action-btn-danger" ${actionAttrs('deleteFieldConfig', [doctypeName, f.id])}>Delete</button>
        </td>
      </tr>
    `).join('');

  html += rowsHTML || `<tr><td colspan="7">No fields defined on this record type yet &mdash; use <b>Add Field</b> above.</td></tr>`;
  html += `</tbody></table>`;
  container.innerHTML = html;
  container.__fields = fields;
};

// --- Schema field add/edit (Stage 41) ---------------------------------
//
// Both verbs go through one modal (#add-field-modal in index.html, which had
// been sitting there unreferenced since it was written). Before this the
// screen had no edit at all and created fields through a chain of six
// prompt() dialogs - each one losing everything typed so far if cancelled,
// with fieldtype typed free-hand against a list the server would then reject.
//
// openFieldModal(doctype, existing) is the single entry point: `existing`
// null = create (POST), a field row = edit (PUT to that field's id).
function openFieldModal(doctypeName, existing) {
  const modal = document.getElementById('add-field-modal');
  if (!modal) return;
  document.getElementById('add-field-doctype').value = doctypeName;
  document.getElementById('add-field-id').value = existing ? existing.id : '';
  document.getElementById('add-field-name').value = existing ? existing.fieldname : '';
  document.getElementById('add-field-label').value = existing ? existing.label : '';
  document.getElementById('add-field-type').value = existing ? existing.fieldtype : 'Data';
  document.getElementById('add-field-mandatory').checked = existing ? !!existing.mandatory : false;
  document.getElementById('add-field-options').value = existing ? (existing.options || '') : '';
  // A new field lands after everything already defined rather than at a fixed
  // 10, which is what the old prompt flow hardcoded - so several added fields
  // no longer all collide on the same order.
  const fields = (document.getElementById('doctype-fields-config') || {}).__fields || [];
  const nextOrder = fields.reduce((m, f) => Math.max(m, Number(f.display_order) || 0), 0) + 1;
  document.getElementById('add-field-order').value = existing ? existing.display_order : nextOrder;

  document.getElementById('add-field-modal-title').textContent =
    existing ? `Edit Field on ${doctypeName}` : `Add Field to ${doctypeName}`;
  document.getElementById('add-field-submit').textContent = existing ? 'Save Changes' : 'Add Field';
  document.getElementById('add-field-rename-warning').classList.toggle('hidden', !existing);
  const err = document.getElementById('add-field-error');
  err.classList.add('hidden');
  err.textContent = '';
  modal.inert = false;
  modal.classList.add('open');
  document.getElementById('add-field-name').focus();
}

window.addNewFieldConfig = function(doctypeName) {
  openFieldModal(doctypeName, null);
};

window.editFieldConfig = function(doctypeName, index) {
  const fields = (document.getElementById('doctype-fields-config') || {}).__fields || [];
  const f = fields[index];
  if (!f) return;
  openFieldModal(doctypeName, f);
};

window.closeAddFieldModal = function() {
  const modal = document.getElementById('add-field-modal');
  if (!modal) return;
  modal.classList.remove('open');
  modal.inert = true;
  document.getElementById('add-field-form').reset();
  document.getElementById('add-field-id').value = '';
};

window.submitAddField = async function(event) {
  event.preventDefault();
  const doctypeName = document.getElementById('add-field-doctype').value;
  const fieldID = document.getElementById('add-field-id').value;
  const errEl = document.getElementById('add-field-error');

  const body = JSON.stringify({
    fieldname: document.getElementById('add-field-name').value.trim(),
    label: document.getElementById('add-field-label').value.trim(),
    fieldtype: document.getElementById('add-field-type').value,
    mandatory: document.getElementById('add-field-mandatory').checked,
    options: document.getElementById('add-field-options').value.trim(),
    display_order: Number(document.getElementById('add-field-order').value) || 0
  });

  const res = fieldID
    ? await apiFetch(`/api/v1/meta/${doctypeName}/fields/${fieldID}`, { method: 'PUT', body })
    : await apiFetch(`/api/v1/meta/${doctypeName}/fields`, { method: 'POST', body });
  if (!res) return;
  if (!res.ok) {
    // Shown inline in the modal, not as a separate dialog over it - the user
    // keeps everything they typed and can correct the one bad value.
    errEl.textContent = await getErrorMessage(res, fieldID ? 'Failed to update field.' : 'Failed to add field.');
    errEl.classList.remove('hidden');
    return;
  }
  closeAddFieldModal();
  // The schema just changed, so the cached doctype/field metadata the record
  // forms render from is now stale.
  await fetchRegisteredDoctypes();
  loadDoctypeConfig(doctypeName);
};

window.deleteFieldConfig = async function(doctypeName, fieldID) {
  if (await showCustomConfirm('Delete this field from record type metadata?')) {
    const res = await apiFetch(`/api/v1/meta/${doctypeName}/fields/${fieldID}`, {
      method: 'DELETE'
    });
    if (!res) return;
    if (res.ok) {
      loadDoctypeConfig(doctypeName);
    } else {
      await showApiError(res, 'Failed to delete field.');
    }
  }
};

// prefixConfigSample renders what the next number of a series will look like,
// mirroring engines/numbering.go's own assembly order:
//   <Prefix><Sep>[<Store><Sep>][<Period><Sep>]<Padded>
// Shown on the admin screen so the effect of a prefix/separator/padding/reset
// change is visible before it is applied to real documents.

export { renderDocTableView, refreshDocTablePage, renderDocTable, isPIMBulkEditDoctype, visibleDocTableItems, updatePIMBulkEditBar, captureFocusForModalReturn, restoreFocusAfterModalClose, handleDynamicFormSubmitInner, renderDocTypeBuilderView, openFieldModal };
