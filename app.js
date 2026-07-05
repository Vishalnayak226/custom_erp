// DitosERP Application State & Logic

// State management
let state = {
  brands: [],
  subBrands: [],
  styles: [],
  subStyles: [],
  productCategories: [],
  productTypes: [],
  itemNames: [],
  colors: [],
  secondaryColors: [],
  fabricColors: [],
  polishes: []
};

// Current view state
let currentView = 'dashboard';
let currentSearchQuery = '';
let currentTablePage = 1;
const itemsPerPage = 10;

// Initialize state
function init() {
  const localData = localStorage.getItem('ditos_erp_data');
  if (localData) {
    state = JSON.parse(localData);
  } else if (window.INITIAL_ERP_DATA) {
    state = JSON.parse(JSON.stringify(window.INITIAL_ERP_DATA));
    saveState();
  }
  
  setupEventListeners();
  renderView(currentView);
}

function saveState() {
  localStorage.setItem('ditos_erp_data', JSON.stringify(state));
}

function setupEventListeners() {
  // Navigation
  document.getElementById('menu-dashboard').addEventListener('click', (e) => {
    e.preventDefault();
    setActiveMenu('menu-dashboard');
    closeSubmenus();
    renderView('dashboard');
  });

  const masterMenuBtn = document.getElementById('menu-master-definition');
  const masterSubmenu = document.getElementById('submenu-master');
  const arrow = masterMenuBtn.querySelector('.menu-item-arrow');

  masterMenuBtn.addEventListener('click', (e) => {
    e.preventDefault();
    const isOpen = masterSubmenu.classList.toggle('open');
    arrow.classList.toggle('rotated', isOpen);
  });

  // Submenu items
  document.querySelectorAll('.submenu-item').forEach(item => {
    item.addEventListener('click', (e) => {
      e.preventDefault();
      document.querySelectorAll('.submenu-item').forEach(i => i.classList.remove('active'));
      document.querySelectorAll('.menu-item').forEach(i => i.classList.remove('active'));
      
      masterMenuBtn.classList.add('active');
      item.classList.add('active');
      
      const view = item.getAttribute('data-view');
      currentSearchQuery = '';
      currentTablePage = 1;
      renderView(view);
    });
  });

  // Main menu items (mock/under construction pages)
  ['menu-vendors', 'menu-stores', 'menu-purchase-orders', 'menu-inventory', 'menu-transfers', 'menu-users', 'menu-roles', 'menu-audit-logs'].forEach(id => {
    const btn = document.getElementById(id);
    if (btn) {
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        setActiveMenu(id);
        closeSubmenus();
        const viewName = id.replace('menu-', '');
        renderView(viewName);
      });
    }
  });

  // Global search input
  const globalSearch = document.getElementById('global-search');
  globalSearch.addEventListener('input', (e) => {
    currentSearchQuery = e.target.value.toLowerCase();
    currentTablePage = 1;
    if (currentView !== 'dashboard') {
      renderTable(currentView);
    }
  });

  // Sync / Reset button
  document.getElementById('sync-btn').addEventListener('click', () => {
    if (confirm('Are you sure you want to reset the database to factory settings? This will delete all customized master definitions.')) {
      localStorage.removeItem('ditos_erp_data');
      state = JSON.parse(JSON.stringify(window.INITIAL_ERP_DATA));
      saveState();
      renderView(currentView);
    }
  });
}

function setActiveMenu(menuId) {
  document.querySelectorAll('.menu-item').forEach(item => item.classList.remove('active'));
  document.querySelectorAll('.submenu-item').forEach(item => item.classList.remove('active'));
  const activeMenu = document.getElementById(menuId);
  if (activeMenu) activeMenu.classList.add('active');
}

function closeSubmenus() {
  document.getElementById('submenu-master').classList.remove('open');
  const arrow = document.querySelector('#menu-master-definition .menu-item-arrow');
  if (arrow) arrow.classList.remove('rotated');
}

// Router
function renderView(view) {
  currentView = view;
  const root = document.getElementById('view-root');
  root.innerHTML = ''; // Clear current view
  
  if (view === 'dashboard') {
    renderDashboard(root);
  } else if (state[view]) {
    renderMasterTableView(root, view);
  } else {
    // Other modules fallback dashboard card mock-ups
    renderMockModuleView(root, view);
  }
}

// Render Dashboard Grid
function renderDashboard(container) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">Dashboard</h1>
      <p class="page-subtitle">Welcome to DitosERP. Choose a module to get started.</p>
    </div>
  `;
  container.appendChild(header);

  const grid = document.createElement('div');
  grid.className = 'dashboard-grid';

  const modules = [
    { id: 'masterData', title: 'Master Data', desc: 'Manage brands, styles, colors, sizes and more', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/></svg>`, action: () => triggerMasterSubmenu() },
    { id: 'designs', title: 'Designs', desc: 'Create and manage product designs', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>` },
    { id: 'combinations', title: 'Combinations', desc: 'Manage product combinations linked to designs', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16.5 9.4 7.55 4.24a1.79 1.79 0 0 0-2.5.76L2.3 9.4a1.79 1.79 0 0 0 .76 2.49l8.95 5.16a1.79 1.79 0 0 0 2.5-.76l2.75-4.4a1.79 1.79 0 0 0-.76-2.49Z"/></svg>` },
    { id: 'vendors', title: 'Vendors', desc: 'Manage suppliers, job workers and logistics partners', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="1" y="3" width="15" height="13"></rect><polygon points="16 8 20 8 23 11 23 16 16 16 16 8"></polygon><circle cx="5.5" cy="18.5" r="2.5"></circle><circle cx="18.5" cy="18.5" r="2.5"></circle></svg>`, action: () => { setActiveMenu('menu-vendors'); renderView('vendors'); } },
    { id: 'stores', title: 'Stores', desc: 'Manage store locations and details', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path></svg>`, action: () => { setActiveMenu('menu-stores'); renderView('stores'); } },
    { id: 'purchaseOrders', title: 'Purchase Orders', desc: 'Create and track purchase orders', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline></svg>`, action: () => { setActiveMenu('menu-purchase-orders'); renderView('purchase-orders'); } },
    { id: 'inventory', title: 'Inventory', desc: 'View Inventory and manage stock locations', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4a2 2 0 0 0 1-1.73z"></path></svg>`, action: () => { setActiveMenu('menu-inventory'); renderView('inventory'); } },
    { id: 'transfers', title: 'Transfers', desc: 'Transfer stock between store locations', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="17 1 21 5 17 9"></polyline><path d="M3 11V9a4 4 0 0 1 4-4h14"></path></svg>`, action: () => { setActiveMenu('menu-transfers'); renderView('transfers'); } },
    { id: 'users', title: 'Users', desc: 'Manage user accounts and access', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path><circle cx="9" cy="7" r="4"></circle></svg>`, action: () => { setActiveMenu('menu-users'); renderView('users'); } },
    { id: 'roles', title: 'Roles', desc: 'Define roles and assign permissions', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>`, action: () => { setActiveMenu('menu-roles'); renderView('roles'); } },
    { id: 'auditLogs', title: 'Audit Logs', desc: 'Track user activity and changes', icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><line x1="16" y1="13" x2="8" y2="13"></line></svg>`, action: () => { setActiveMenu('menu-audit-logs'); renderView('audit-logs'); } }
  ];

  modules.forEach(m => {
    const card = document.createElement('div');
    card.className = 'dashboard-card';
    card.innerHTML = `
      <div class="card-icon">${m.icon}</div>
      <div class="card-content">
        <h3 class="card-title">${m.title}</h3>
        <p class="card-desc">${m.desc}</p>
      </div>
    `;
    card.addEventListener('click', m.action || (() => alert(`${m.title} module is ready for schema definition.`)));
    grid.appendChild(card);
  });

  container.appendChild(grid);
}

function triggerMasterSubmenu() {
  const masterSubmenu = document.getElementById('submenu-master');
  const arrow = document.querySelector('#menu-master-definition .menu-item-arrow');
  masterSubmenu.classList.add('open');
  if (arrow) arrow.classList.add('rotated');
  
  // Click first submenu item (Brands)
  const firstItem = masterSubmenu.querySelector('.submenu-item');
  if (firstItem) firstItem.click();
}

// Master Table Screen
function renderMasterTableView(container, entityType) {
  const displayName = getEntityDisplayName(entityType);
  
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">${displayName}</h1>
      <p class="page-subtitle">Manage system definitions, codes, status, and linked relationships.</p>
    </div>
    <button class="btn btn-primary" onclick="openModal('${entityType}')">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
      <span>New ${getEntitySingleName(entityType)}</span>
    </button>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.innerHTML = `
    <div class="table-controls">
      <div class="search-box">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" stroke-width="2">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <input type="text" placeholder="Search table..." oninput="handleTableSearch(event)">
      </div>
      <div class="table-actions">
        <button class="btn btn-outline btn-sm" onclick="exportCSV('${entityType}')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          Export CSV
        </button>
      </div>
    </div>
    <div class="table-wrapper" id="table-wrapper-${entityType}"></div>
    <div class="pagination" id="pagination-${entityType}"></div>
  `;
  container.appendChild(panel);

  renderTable(entityType);
}

function handleTableSearch(e) {
  currentSearchQuery = e.target.value.toLowerCase();
  currentTablePage = 1;
  renderTable(currentView);
}

// Render actual table contents
function renderTable(entityType) {
  const wrapper = document.getElementById(`table-wrapper-${entityType}`);
  const paginationContainer = document.getElementById(`pagination-${entityType}`);
  if (!wrapper) return;

  const dataList = state[entityType] || [];
  
  // Filter search
  const filteredList = dataList.filter(item => {
    const codeMatch = item.code && item.code.toLowerCase().includes(currentSearchQuery);
    const nameMatch = item.name && item.name.toLowerCase().includes(currentSearchQuery);
    
    // Relation matches
    let parentMatch = false;
    if (entityType === 'subBrands' && item.brandId) {
      const parent = state.brands.find(b => b.id === item.brandId);
      parentMatch = parent && parent.name.toLowerCase().includes(currentSearchQuery);
    } else if (entityType === 'subStyles' && item.styleId) {
      const parent = state.styles.find(s => s.id === item.styleId);
      parentMatch = parent && parent.name.toLowerCase().includes(currentSearchQuery);
    } else if (entityType === 'productTypes' && item.categoryId) {
      const parent = state.productCategories.find(c => c.id === item.categoryId);
      parentMatch = parent && parent.name.toLowerCase().includes(currentSearchQuery);
    } else if (entityType === 'itemNames') {
      const category = state.productCategories.find(c => c.id === item.categoryId);
      const type = state.productTypes.find(t => t.id === item.productTypeId);
      parentMatch = (category && category.name.toLowerCase().includes(currentSearchQuery)) || 
                    (type && type.name.toLowerCase().includes(currentSearchQuery));
    }
    
    return codeMatch || nameMatch || parentMatch;
  });

  // Pagination logic
  const totalItems = filteredList.length;
  const totalPages = Math.ceil(totalItems / itemsPerPage) || 1;
  if (currentTablePage > totalPages) currentTablePage = totalPages;
  const startIndex = (currentTablePage - 1) * itemsPerPage;
  const endIndex = Math.min(startIndex + itemsPerPage, totalItems);
  const paginatedList = filteredList.slice(startIndex, endIndex);

  // Columns specification
  let columns = [];
  if (entityType === 'brands' || entityType === 'styles' || entityType === 'colors' || 
      entityType === 'secondaryColors' || entityType === 'fabricColors' || entityType === 'polishes') {
    columns = [
      { key: 'code', label: 'Code' },
      { key: 'name', label: 'Name' },
      { key: 'status', label: 'Status' }
    ];
  } else if (entityType === 'subBrands') {
    columns = [
      { key: 'code', label: 'Code' },
      { key: 'brand', label: 'Brand', format: (item) => {
          const parent = state.brands.find(b => b.id === item.brandId);
          return parent ? parent.name : 'Unknown';
        }
      },
      { key: 'name', label: 'Sub Brand Name' },
      { key: 'status', label: 'Status' }
    ];
  } else if (entityType === 'subStyles') {
    columns = [
      { key: 'code', label: 'Code' },
      { key: 'style', label: 'Style', format: (item) => {
          const parent = state.styles.find(s => s.id === item.styleId);
          return parent ? parent.name : 'Unknown';
        }
      },
      { key: 'name', label: 'Sub Style Name' },
      { key: 'status', label: 'Status' }
    ];
  } else if (entityType === 'productCategories') {
    columns = [
      { key: 'code', label: 'Code' },
      { key: 'name', label: 'Name' },
      { key: 'isWeight', label: 'Is Weight', format: (item) => item.isWeight ? 'Yes' : 'No' },
      { key: 'isNetWeight', label: 'Is Net Weight', format: (item) => item.isNetWeight ? 'Yes' : 'No' },
      { key: 'status', label: 'Status' }
    ];
  } else if (entityType === 'productTypes') {
    columns = [
      { key: 'code', label: 'Code' },
      { key: 'name', label: 'Name' },
      { key: 'category', label: 'Product Category', format: (item) => {
          const parent = state.productCategories.find(c => c.id === item.categoryId);
          return parent ? parent.name : 'None';
        }
      },
      { key: 'description', label: 'Description' },
      { key: 'status', label: 'Status' }
    ];
  } else if (entityType === 'itemNames') {
    columns = [
      { key: 'category', label: 'Category', format: (item) => {
          const parent = state.productCategories.find(c => c.id === item.categoryId);
          return parent ? parent.name : 'None';
        }
      },
      { key: 'type', label: 'Product Type', format: (item) => {
          const parent = state.productTypes.find(t => t.id === item.productTypeId);
          return parent ? parent.name : 'None';
        }
      },
      { key: 'hsnCode', label: 'HSN Code' },
      { key: 'stickerType', label: 'Sticker Type' },
      { key: 'name', label: 'Item Name' }
    ];
  }

  // Draw Table
  let tableHTML = `
    <table>
      <thead>
        <tr>
          ${columns.map(c => `<th>${c.label}</th>`).join('')}
          <th style="text-align: right;">Actions</th>
        </tr>
      </thead>
      <tbody>
  `;

  if (paginatedList.length === 0) {
    tableHTML += `
      <tr>
        <td colspan="${columns.length + 1}" class="text-center text-muted py-8">
          No records found.
        </td>
      </tr>
    `;
  } else {
    paginatedList.forEach(item => {
      tableHTML += `<tr>`;
      columns.forEach(col => {
        let val = '';
        if (col.format) {
          val = col.format(item);
        } else {
          val = item[col.key] || '';
        }

        if (col.key === 'status') {
          const badgeClass = val === 'Active' ? 'badge-success' : 'badge-secondary';
          tableHTML += `<td><span class="badge ${badgeClass}">${val}</span></td>`;
        } else if (col.key === 'code') {
          tableHTML += `<td style="font-family: monospace; font-weight: 600; color: var(--primary-color);">${val}</td>`;
        } else {
          tableHTML += `<td>${val}</td>`;
        }
      });
      
      tableHTML += `
        <td style="text-align: right;">
          <button class="action-btn" onclick="toggleItemStatus('${entityType}', '${item.id}')" title="Toggle Active Status">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
          </button>
          <button class="action-btn action-btn-danger" onclick="deleteItem('${entityType}', '${item.id}')" title="Delete Item">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
          </button>
        </td>
      </tr>`;
    });
  }

  tableHTML += `</tbody></table>`;
  wrapper.innerHTML = tableHTML;

  // Pagination HTML
  paginationContainer.innerHTML = `
    <span>Showing ${totalItems === 0 ? 0 : startIndex + 1}-${endIndex} of ${totalItems}</span>
    <div class="pagination-buttons">
      <button class="pagination-btn" onclick="changeTablePage('${entityType}', ${currentTablePage - 1})" ${currentTablePage === 1 ? 'disabled' : ''}>Previous</button>
      <button class="pagination-btn" onclick="changeTablePage('${entityType}', ${currentTablePage + 1})" ${currentTablePage === totalPages ? 'disabled' : ''}>Next</button>
    </div>
  `;
}

window.changeTablePage = function(entityType, newPage) {
  currentTablePage = newPage;
  renderTable(entityType);
};

// Toggle active status
window.toggleItemStatus = function(entityType, id) {
  const item = state[entityType].find(i => i.id === id);
  if (item && item.hasOwnProperty('status')) {
    item.status = item.status === 'Active' ? 'Inactive' : 'Active';
    saveState();
    renderTable(entityType);
  } else {
    alert("Status field is not applicable for this entity.");
  }
};

// Delete record
window.deleteItem = function(entityType, id) {
  if (confirm(`Are you sure you want to delete this ${getEntitySingleName(entityType)}?`)) {
    state[entityType] = state[entityType].filter(i => i.id !== id);
    saveState();
    renderTable(entityType);
  }
};

// Export to CSV helper
window.exportCSV = function(entityType) {
  const data = state[entityType] || [];
  if (data.length === 0) {
    alert('No data to export.');
    return;
  }
  
  let csvContent = "data:text/csv;charset=utf-8,";
  // headers
  const headers = Object.keys(data[0]).filter(k => k !== 'id');
  csvContent += headers.join(",") + "\r\n";
  
  // rows
  data.forEach(item => {
    const row = headers.map(header => {
      let val = item[header];
      if (typeof val === 'string' && val.includes(',')) {
        val = `"${val}"`;
      }
      return val;
    });
    csvContent += row.join(",") + "\r\n";
  });
  
  const encodedUri = encodeURI(csvContent);
  const link = document.createElement("a");
  link.setAttribute("href", encodedUri);
  link.setAttribute("download", `${entityType}_export.csv`);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
};

// View Mock screen for non-master modules
function renderMockModuleView(container, view) {
  const header = document.createElement('div');
  header.className = 'page-header';
  header.innerHTML = `
    <div class="page-title-section">
      <h1 class="page-title">${view.charAt(0).toUpperCase() + view.slice(1).replace('-', ' ')}</h1>
      <p class="page-subtitle">This workspace is currently configured for Master Definition editing.</p>
    </div>
  `;
  container.appendChild(header);

  const panel = document.createElement('div');
  panel.className = 'table-panel';
  panel.style.padding = '48px';
  panel.style.textAlign = 'center';
  panel.innerHTML = `
    <div style="max-width: 480px; margin: 0 auto; display: flex; flex-direction: column; gap: 16px; align-items: center;">
      <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" stroke-width="1.5">
        <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
      </svg>
      <h2 style="font-size: 20px; font-weight: 600;">Module Setup Pending</h2>
      <p class="text-muted" style="font-size: 14px; line-height: 1.6;">
        To configure this screen, place transaction snapshots (like PO format, GRN receipts, VMS sheets) in the workspace directory. Use the sidebar to inspect and configure live **Master Definition** schema rules.
      </p>
      <button class="btn btn-secondary" onclick="setActiveMenu('menu-dashboard'); renderView('dashboard');">Back to Dashboard</button>
    </div>
  `;
  container.appendChild(panel);
}

// Modal handling
window.openModal = function(entityType) {
  const modal = document.getElementById(`modal-${entityType}`);
  if (!modal) return;

  // Prepare select dropdown lists
  if (entityType === 'subBrand') {
    const parentSelect = document.getElementById('subBrand-parent');
    parentSelect.innerHTML = '<option value="" disabled selected>Select Brand</option>';
    state.brands.forEach(b => {
      parentSelect.innerHTML += `<option value="${b.id}">${b.name}</option>`;
    });
  } else if (entityType === 'subStyle') {
    const parentSelect = document.getElementById('subStyle-parent');
    parentSelect.innerHTML = '<option value="" disabled selected>Select Style</option>';
    state.styles.forEach(s => {
      parentSelect.innerHTML += `<option value="${s.id}">${s.name}</option>`;
    });
  } else if (entityType === 'productType') {
    const parentSelect = document.getElementById('productType-parent');
    parentSelect.innerHTML = '<option value="" disabled selected>Select Category</option>';
    state.productCategories.forEach(c => {
      parentSelect.innerHTML += `<option value="${c.id}">${c.name}</option>`;
    });
  } else if (entityType === 'itemName') {
    const categorySelect = document.getElementById('itemName-category');
    categorySelect.innerHTML = '<option value="" disabled selected>— Select category —</option>';
    state.productCategories.forEach(c => {
      categorySelect.innerHTML += `<option value="${c.id}">${c.name}</option>`;
    });

    const typeSelect = document.getElementById('itemName-type');
    typeSelect.innerHTML = '<option value="" disabled selected>— Select product type —</option>';
    state.productTypes.forEach(t => {
      typeSelect.innerHTML += `<option value="${t.id}">${t.name}</option>`;
    });
  }

  modal.classList.add('open');
};

window.closeModal = function(entityType) {
  const modal = document.getElementById(`modal-${entityType}`);
  if (modal) {
    modal.classList.remove('open');
    const form = document.getElementById(`form-${entityType}`);
    if (form) form.reset();
  }
};

window.handleFormSubmit = function(event, entityType) {
  event.preventDefault();
  
  let newObj = {
    id: 'id_' + Date.now().toString(),
    status: 'Active'
  };

  const count = state[entityType] ? state[entityType].length : 0;
  
  if (entityType === 'brand') {
    newObj.name = document.getElementById('brand-name').value;
    newObj.code = 'BRD' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'subBrand') {
    newObj.brandId = document.getElementById('subBrand-parent').value;
    newObj.name = document.getElementById('subBrand-name').value;
    newObj.code = 'SBRD' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'style') {
    newObj.name = document.getElementById('style-name').value;
    newObj.code = 'STY' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'subStyle') {
    newObj.styleId = document.getElementById('subStyle-parent').value;
    newObj.name = document.getElementById('subStyle-name').value;
    newObj.code = 'SST' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'productCategory') {
    newObj.name = document.getElementById('productCategory-name').value;
    newObj.isWeight = document.getElementById('productCategory-isWeight').checked;
    newObj.isNetWeight = document.getElementById('productCategory-isNetWeight').checked;
    newObj.code = 'CAT' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'productType') {
    newObj.name = document.getElementById('productType-name').value;
    newObj.categoryId = document.getElementById('productType-parent').value;
    newObj.description = document.getElementById('productType-desc').value;
    newObj.code = 'TYP' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'itemName') {
    newObj.categoryId = document.getElementById('itemName-category').value;
    newObj.productTypeId = document.getElementById('itemName-type').value;
    newObj.hsnCode = document.getElementById('itemName-hsn').value || '—';
    newObj.stickerType = document.getElementById('itemName-sticker').value || '—';
    
    // Auto name fallback
    let customName = document.getElementById('itemName-name').value.trim();
    if (!customName) {
      const category = state.productCategories.find(c => c.id === newObj.categoryId);
      const type = state.productTypes.find(t => t.id === newObj.productTypeId);
      customName = `${category ? category.name : ''} ${type ? type.name : ''}`.trim() || 'Unnamed Item';
    }
    newObj.name = customName;
    newObj.code = 'ITM' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'color') {
    newObj.name = document.getElementById('color-name').value;
    newObj.code = 'COL' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'secondaryColor') {
    newObj.name = document.getElementById('secondaryColor-name').value;
    newObj.code = 'SCOL' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'fabricColor') {
    newObj.name = document.getElementById('fabricColor-name').value;
    newObj.code = 'FCOL' + String(count + 1).padStart(2, '0');
  } else if (entityType === 'polish') {
    newObj.name = document.getElementById('polish-name').value;
    newObj.code = 'POL' + String(count + 1).padStart(2, '0');
  }

  // Push to local state and update
  if (!state[entityType]) {
    state[entityType] = [];
  }
  state[entityType].push(newObj);
  saveState();
  
  closeModal(entityType);
  renderTable(entityType);
};

// Entity display helper functions
function getEntityDisplayName(type) {
  const mapping = {
    brands: 'Brands',
    subBrands: 'Sub Brands',
    styles: 'Styles',
    subStyles: 'Sub Styles',
    productCategories: 'Product Categories',
    productTypes: 'Product Types',
    itemNames: 'Item Names',
    colors: 'Colors',
    secondaryColors: 'Secondary Colors',
    fabricColors: 'Fabric Colors',
    polishes: 'Polishes'
  };
  return mapping[type] || type;
}

function getEntitySingleName(type) {
  const mapping = {
    brands: 'Brand',
    subBrands: 'Sub Brand',
    styles: 'Style',
    subStyles: 'Sub Style',
    productCategories: 'Product Category',
    productTypes: 'Product Type',
    itemNames: 'Item Name',
    colors: 'Color',
    secondaryColors: 'Secondary Color',
    fabricColors: 'Fabric Color',
    polishes: 'Polish'
  };
  return mapping[type] || type.slice(0, -1);
}

// Start the application
window.onload = init;
