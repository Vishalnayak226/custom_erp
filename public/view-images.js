// Stage 95: Design and Combination ID images.
//  - renderPOImages: the PO's Images page (Purchase Orders -> Images on a
//    row) - its designs, each with the combinations ordered under it; add
//    images per design / combination, or drop a folder of photos and each
//    file goes to the ID it is named after.
//  - attachRecordImages: the same image strip on the Design and Item forms.
//  - renderImageDriveSection: Configuration -> Integrations -> Image drive,
//    the client's own S3-compatible bucket the images are stored on.
// Server side: engines/owner_images.go, engines/image_drive.go,
// internal/server/handlers_images.go.

const QUERY = new URL(import.meta.url).search;
(() => {
  const id = 'view-images-css';
  if (document.getElementById(id)) return;
  const link = document.createElement('link');
  link.id = id;
  link.rel = 'stylesheet';
  link.href = '/view-images.css' + QUERY;
  document.head.appendChild(link);
})();

const esc = v => escapeHTMLText(v == null ? '' : String(v));
const IMAGE_ACCEPT = 'image/jpeg,image/png,image/webp,image/gif';
const MAIN = 'Main Image';

// --- thumbnails --------------------------------------------------------------
// An <img> cannot send the session's token, so each thumbnail is fetched as a
// blob - four at a time, so a 40-image PO does not fire 40 requests at once -
// and kept for the rest of the session (an image id never changes its image).
const thumbURLs = new Map();
const thumbQueue = [];
let thumbsLoading = 0;

function pumpThumbs() {
  while (thumbsLoading < 4 && thumbQueue.length) {
    const img = thumbQueue.shift();
    if (!img.isConnected) continue;
    thumbsLoading++;
    loadThumb(img).finally(() => { thumbsLoading--; pumpThumbs(); });
  }
}

async function loadThumb(img) {
  const id = img.dataset.imgId;
  let url = thumbURLs.get(id);
  if (!url) {
    const res = await apiFetch(`/api/v1/images/${encodeURIComponent(id)}/thumbnail`);
    if (!res || !res.ok) {
      const tile = img.closest('.img-tile');
      if (tile) tile.classList.add('is-broken');
      img.alt = 'This image could not be loaded';
      return;
    }
    url = URL.createObjectURL(await res.blob());
    thumbURLs.set(id, url);
  }
  img.src = url;
}

function fillThumbs(root) {
  root.querySelectorAll('img[data-img-id]').forEach(img => thumbQueue.push(img));
  pumpThumbs();
}

async function openLightbox(mediaId, label) {
  const res = await apiFetch(`/api/v1/images/${encodeURIComponent(mediaId)}/file`);
  if (!res) return;
  if (!res.ok) { await showApiError(res, 'This image could not be opened.', 'Image'); return; }
  const url = URL.createObjectURL(await res.blob());
  const box = document.createElement('div');
  box.className = 'img-lightbox';
  box.setAttribute('role', 'dialog');
  box.setAttribute('aria-label', label || 'Image');
  box.tabIndex = -1;
  box.innerHTML = `<img src="${url}" alt="${esc(label)}">`;
  const close = () => { box.remove(); URL.revokeObjectURL(url); document.removeEventListener('keydown', onKey); };
  const onKey = e => { if (e.key === 'Escape') close(); };
  box.addEventListener('click', close);
  document.addEventListener('keydown', onKey);
  document.body.appendChild(box);
  box.focus();
}

// --- one owner's strip -------------------------------------------------------

function optionsText(options) {
  return Object.entries(options || {}).filter(([, v]) => v).map(([k, v]) => `${k.replace(/_/g, ' ')}: ${v}`).join(' · ');
}

function tileHtml(m, label, editable) {
  const main = m.media_role === MAIN;
  const actions = editable
    ? `<div class="img-tile-actions">${main ? '' : `<button type="button" class="btn btn-outline btn-sm" data-img-main="${esc(m.id)}">Make main</button>`}<button type="button" class="btn btn-outline btn-sm" data-img-remove="${esc(m.id)}">Remove</button></div>`
    : '';
  return `<figure class="img-tile${main ? ' is-main' : ''}">
      <button type="button" class="img-tile-view" data-img-view="${esc(m.id)}" data-img-label="${esc(label)}" aria-label="Open image of ${esc(label)}"><img data-img-id="${esc(m.id)}" alt="${esc(label)}"></button>
      ${main ? '<span class="img-tile-badge">Main</span>' : ''}${actions}
    </figure>`;
}

function stripHtml(owner, editable) {
  const images = owner.images || [];
  const label = owner.code;
  return `<div class="img-strip">
      ${images.map(m => tileHtml(m, label, editable)).join('')}
      ${editable ? `<button type="button" class="img-add" data-img-add="${esc(owner.doctype)}" data-img-code="${esc(owner.code)}">+ Add images</button>` : ''}
      ${!editable && !images.length ? '<span class="img-none">No images</span>' : ''}
    </div>`;
}

function ownerHeadHtml(owner, kind) {
  const opts = optionsText(owner.options);
  return `<div class="img-owner-head">
      <span class="img-kind">${esc(kind)}</span>
      <span class="img-code">${esc(owner.code)}</span>
      ${owner.name ? `<span class="img-name">${esc(owner.name)}</span>` : ''}
      ${opts ? `<span class="img-name">${esc(opts)}</span>` : ''}
    </div>`;
}

// uploadEach sends files one at a time (a 429 or a lost connection stops the
// run; what was not sent is listed so nothing silently disappears).
async function uploadEach(files, buildForm, url, onProgress) {
  const results = [];
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    onProgress(`Uploading ${i + 1} of ${files.length}: ${file.name}`);
    const res = await apiUpload(url, buildForm(file));
    if (!res) {
      for (let j = i; j < files.length; j++) results.push({ file: files[j].name, error: 'Not uploaded - add it again', blob: files[j] });
      break;
    }
    if (!res.ok) {
      results.push({ file: file.name, error: await getErrorMessage(res, 'Not uploaded'), blob: file });
      continue;
    }
    const body = await res.json().catch(() => ({}));
    results.push({ ...body, file: file.name, blob: file, matched: body.matched !== false });
  }
  onProgress('');
  return results;
}

// pickImages opens the file chooser. One picker on <body>, so a page that
// repaints itself cannot take it out of the document mid-choice.
let pickerDone = null;
function pickImages(done) {
  let picker = document.getElementById('img-picker');
  if (!picker) {
    picker = document.createElement('input');
    picker.type = 'file';
    picker.id = 'img-picker';
    picker.accept = IMAGE_ACCEPT;
    picker.multiple = true;
    picker.hidden = true;
    picker.addEventListener('change', () => {
      const files = [...picker.files];
      const cb = pickerDone;
      picker.value = '';
      pickerDone = null;
      if (files.length && cb) cb(files);
    });
    document.body.appendChild(picker);
  }
  pickerDone = done;
  picker.click();
}

// wireStrips: Add / Make main / Remove / open, for every strip under root.
// The image calls are passed in, so the PO page (PO permission) and the
// Design / Item form (that record's permission) share one strip.
function wireStrips(root, api) {
  root.addEventListener('click', async e => {
    const el = e.target.closest('[data-img-add],[data-img-main],[data-img-remove],[data-img-view]');
    if (!el || !root.contains(el)) return;
    if (el.dataset.imgAdd) {
      const target = { doctype: el.dataset.imgAdd, code: el.dataset.imgCode };
      pickImages(files => api.add(files, target));
    } else if (el.dataset.imgView) {
      if (!el.closest('.img-tile.is-broken')) openLightbox(el.dataset.imgView, el.dataset.imgLabel);
    } else if (el.dataset.imgMain) {
      const id = el.dataset.imgMain;
      el.disabled = true;
      await api.action('main', id);
    } else if (el.dataset.imgRemove) {
      const id = el.dataset.imgRemove;
      if (!await showCustomConfirm('Remove this image? It is kept in the image history, not deleted.', 'Remove image')) return;
      await api.action('remove', id);
    }
  });
}

async function postImageAction(url, mediaId, failText) {
  const res = await apiFetch(url, { method: 'POST', body: JSON.stringify({ media_id: mediaId }) });
  if (!res) return false;
  if (!res.ok) { await showApiError(res, failText, 'Images'); return false; }
  return true;
}

// --- the PO's Images page ----------------------------------------------------

function storageChip(p) {
  return p.storage === 'drive'
    ? `<span class="img-where">Stored on <b>your image drive</b> (${esc(p.bucket)})</span>`
    : '<span class="img-where">Stored on <b>the ERP server</b></span>';
}

function poOwners(p) {
  const out = [];
  (p.designs || []).forEach(d => { out.push({ ...d, kind: 'Design' }); (d.combinations || []).forEach(c => out.push({ ...c, kind: 'Combination' })); });
  (p.items || []).forEach(i => out.push({ ...i, kind: 'Item' }));
  return out;
}

function resultsHtml(results, owners) {
  if (!results.length) return '';
  const added = results.filter(r => r.matched && !r.error);
  const missed = results.filter(r => !r.matched || r.error);
  const choices = owners.map(o => `<option value="${esc(o.doctype)}|${esc(o.code)}">${esc(o.kind)} ${esc(o.code)}</option>`).join('');
  return `<div class="img-results" role="status">
      <b>${added.length} added</b>${missed.length ? `, <b class="is-miss">${missed.length} not added</b>` : ''}
      <ul>
        ${missed.map((r, i) => `<li><span class="is-miss">${esc(r.file)}</span><span>${esc(r.error || r.reason || 'No design or combination on this PO is named like this file')}</span>
          ${r.blob ? `<select class="form-input" data-img-assign="${i}" aria-label="Add ${esc(r.file)} to"><option value="">Add it to...</option>${choices}</select>` : ''}</li>`).join('')}
        ${added.map(r => `<li><span class="is-hit">${esc(r.file)}</span><span>→ ${esc(r.code)}${r.image && r.image.media_role === MAIN ? ' (main)' : ''}</span></li>`).join('')}
      </ul>
    </div>`;
}

export async function renderPOImages(screen, poId, { head }) {
  screen.innerHTML = `<div class="txn-bar">${head('')}</div><div class="img-page"><p class="text-muted">Loading images…</p></div>`;
  const res = await apiFetch(`/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/images`);
  if (!res) return;
  if (!res.ok) {
    screen.querySelector('.img-page').innerHTML = `<p style="color: var(--danger-color);">${esc(await getErrorMessage(res, 'The images for this purchase order could not be loaded.'))}</p>`;
    return;
  }
  const p = await res.json();
  const editable = p.status !== 'Cancelled';
  const uploadUrl = `/api/v1/procurement/purchase-order/${encodeURIComponent(poId)}/images`;
  let lastResults = [];

  const paint = () => {
    const owners = poOwners(p);
    const designs = (p.designs || []).map(d => `
      <section class="img-owner">
        ${ownerHeadHtml(d, 'Design')}
        ${stripHtml(d, editable)}
        <div class="img-combos">
          ${(d.combinations || []).map(c => `<div>${ownerHeadHtml(c, 'Combination')}${stripHtml(c, editable)}</div>`).join('')}
        </div>
      </section>`).join('');
    const items = (p.items || []).map(i => `<section class="img-owner">${ownerHeadHtml(i, 'Item')}${stripHtml(i, editable)}</section>`).join('');
    screen.innerHTML = `
      <div class="txn-bar">${head(p.po_number)}${storageChip(p)}</div>
      <div class="img-page">
        ${editable && owners.length ? `<div class="img-drop" data-img-drop>
            Drop photos here or <button type="button" class="btn btn-outline btn-sm" data-img-choose>Choose files</button>
            - each goes to the Design or Combination ID it is named after, e.g. <code>000006.jpg</code>, <code>000006-Red-S-2.jpg</code>
            <div class="img-progress" aria-live="polite"></div>
          </div>` : ''}
        <div data-img-results>${resultsHtml(lastResults, owners)}</div>
        ${designs}${items}
        ${owners.length ? '' : '<p class="text-muted">This purchase order has no lines with items yet.</p>'}
      </div>`;
    fillThumbs(screen);
    wireAssign();
  };

  const progress = text => { const el = screen.querySelector('.img-progress'); if (el) el.textContent = text; };

  const reload = async () => {
    const again = await apiFetch(uploadUrl);
    if (again && again.ok) Object.assign(p, await again.json());
    paint();
  };

  // send uploads files; keep is the earlier results still to show (the
  // other unmatched files when one is sent to a design by hand).
  const send = async (files, target, keep = []) => {
    const results = await uploadEach(files, file => {
      const fd = new FormData();
      fd.append('file', file);
      if (target) { fd.append('target_doctype', target.doctype); fd.append('target_code', target.code); }
      return fd;
    }, uploadUrl, progress);
    lastResults = keep.concat(results);
    const added = results.filter(r => r.matched && !r.error).length;
    if (added) showToast(`${added} image${added === 1 ? '' : 's'} added.`, { variant: 'success' });
    await reload();
  };

  // A file that matched nothing can be sent to a design or combination by hand.
  function wireAssign() {
    screen.querySelectorAll('[data-img-assign]').forEach(sel => {
      sel.addEventListener('change', async () => {
        const missed = lastResults.filter(r => !r.matched || r.error);
        const r = missed[Number(sel.dataset.imgAssign)];
        if (!r || !sel.value) return;
        const [doctype, ...rest] = sel.value.split('|');
        sel.disabled = true;
        await send([r.blob], { doctype, code: rest.join('|') }, lastResults.filter(x => x !== r));
      });
    });
  }

  wireStrips(screen, {
    add: (files, target) => send(files, target),
    action: async (kind, mediaId) => {
      if (await postImageAction(`${uploadUrl}/${kind}`, mediaId, kind === 'main' ? 'The main image could not be changed.' : 'The image could not be removed.')) await reload();
      else paint();
    }
  });

  screen.addEventListener('click', e => {
    if (e.target.closest('[data-img-choose]')) pickImages(files => send(files, null));
  });
  screen.addEventListener('dragover', e => {
    const zone = e.target.closest('[data-img-drop]');
    if (!zone) return;
    e.preventDefault();
    zone.classList.add('is-over');
  });
  screen.addEventListener('dragleave', e => {
    const zone = e.target.closest('[data-img-drop]');
    if (zone && !zone.contains(e.relatedTarget)) zone.classList.remove('is-over');
  });
  screen.addEventListener('drop', e => {
    const zone = e.target.closest('[data-img-drop]');
    if (!zone) return;
    e.preventDefault();
    zone.classList.remove('is-over');
    const files = [...(e.dataTransfer && e.dataTransfer.files || [])];
    if (files.length) send(files, null);
  });

  paint();
}

// --- the Design / Item form ----------------------------------------------------

export async function attachRecordImages(body, doctype, record) {
  const code = record && (record.code || record.id);
  if (!body || !code) return;
  const section = document.createElement('details');
  section.className = 'form-section';
  section.open = true;
  section.innerHTML = '<summary>Images</summary><div class="img-record-body"><span class="img-none">Loading images…</span></div>';
  body.appendChild(section);
  const host = section.querySelector('.img-record-body');
  const owner = { doctype, code, images: [] };

  const paint = () => { host.innerHTML = stripHtml(owner, true); fillThumbs(host); };
  const reload = async () => {
    const res = await apiFetch(`/api/v1/images?doctype=${encodeURIComponent(doctype)}&code=${encodeURIComponent(code)}`);
    if (!res || !res.ok) { host.innerHTML = '<span class="img-none">Images could not be loaded.</span>'; return; }
    owner.images = await res.json();
    paint();
  };

  wireStrips(section, {
    add: async files => {
      const results = await uploadEach(files, file => {
        const fd = new FormData();
        fd.append('file', file);
        fd.append('doctype', doctype);
        fd.append('code', code);
        return fd;
      }, '/api/v1/images', () => {});
      const failed = results.filter(r => r.error);
      if (failed.length) await showCustomAlert(failed.map(r => `${r.file}: ${r.error}`).join('\n'), 'Images not added');
      const added = results.length - failed.length;
      if (added) showToast(`${added} image${added === 1 ? '' : 's'} added - saved already, no need to press Save.`, { variant: 'success' });
      await reload();
    },
    action: async (kind, mediaId) => {
      await postImageAction(`/api/v1/images/${kind}`, mediaId, kind === 'main' ? 'The main image could not be changed.' : 'The image could not be removed.');
      await reload();
    }
  });
  await reload();
}

// --- Configuration -> Integrations -> Image drive ----------------------------

// Stage 95.9: the client's own two folders in the bucket - design photos go
// to <Design images folder>/<Design ID>/, combination photos to
// <Combination images folder>/<Combination ID>/.
const DRIVE_FIELDS = [
  { name: 'endpoint', label: 'Endpoint (S3 API)', wide: true, placeholder: 'https://<account-id>.r2.cloudflarestorage.com' },
  { name: 'region', label: 'Region', placeholder: 'auto' },
  { name: 'bucket', label: 'Bucket', placeholder: 'erp-images' },
  { name: 'design_folder', label: 'Design images folder', placeholder: 'Image/Design Image' },
  { name: 'combination_folder', label: 'Combination images folder', placeholder: 'Image/Combination Image' },
  { name: 'access_key_id', label: 'Access key ID' },
  { name: 'secret_access_key', label: 'Secret access key', secret: true }
];

function driveStateHtml(v) {
  if (!v.configured) return '<div class="img-drive-state">Not set up - images are kept on the ERP server.</div>';
  if (v.enabled) return `<div class="img-drive-state is-on">On - new Design and Combination ID images go to <b>${esc(v.bucket)}</b>.</div>`;
  return '<div class="img-drive-state">Off - new images go to the ERP server; images already on the drive still show.</div>';
}

export async function renderImageDriveSection(host) {
  host.innerHTML = '<section class="img-drive"><h3>Image drive</h3><span class="img-none">Loading…</span></section>';
  const res = await apiFetch('/api/v1/admin/image-drive');
  if (!res || !res.ok) {
    host.innerHTML = '<section class="img-drive"><h3>Image drive</h3><div class="img-drive-state">Only an administrator can set the image drive.</div></section>';
    return;
  }
  const v = await res.json();
  const inputs = DRIVE_FIELDS.map(f => `
    <div class="form-group${f.wide ? ' is-wide' : ''}" style="margin:0 0 12px;">
      <label class="form-label" for="imgdrive-${f.name}">${esc(f.label)}</label>
      <input type="${f.secret ? 'password' : 'text'}" class="form-input" id="imgdrive-${f.name}" autocomplete="off"
             value="${f.secret ? '' : esc(v[f.name] || '')}"
             placeholder="${esc(f.secret && v.secret_set ? 'Stored - leave blank to keep it' : (f.placeholder || ''))}">
    </div>`).join('');
  host.innerHTML = `
    <section class="img-drive">
      <h3>Image drive</h3>
      ${driveStateHtml(v)}
      <div class="img-drive-grid">${inputs}</div>
      <label class="img-drive-check"><input type="checkbox" id="imgdrive-enabled" ${v.enabled ? 'checked' : ''}> Store new images on this drive</label>
      <div class="img-drive-actions">
        <button type="button" class="btn btn-secondary" id="imgdrive-test">Test connection</button>
        <button type="button" class="btn btn-primary" id="imgdrive-save">Save image drive</button>
        <span class="img-none" id="imgdrive-result" aria-live="polite"></span>
      </div>
      <details>
        <summary>Endpoint and region for common providers</summary>
        <ul>
          <li>Design photos go to <code>&lt;Design images folder&gt;/&lt;Design ID&gt;/</code>, combination photos to <code>&lt;Combination images folder&gt;/&lt;Combination ID&gt;/</code> (a <code>/</code> in a number is written <code>-</code>).</li>
          <li>Cloudflare R2: <code>https://&lt;account-id&gt;.r2.cloudflarestorage.com</code>, region <code>auto</code></li>
          <li>DigitalOcean Spaces: <code>https://blr1.digitaloceanspaces.com</code>, region <code>blr1</code></li>
          <li>AWS S3: <code>https://s3.ap-south-1.amazonaws.com</code>, region <code>ap-south-1</code></li>
          <li>Backblaze B2 / Wasabi / MinIO: the S3 endpoint and region the provider shows for the bucket</li>
        </ul>
      </details>
    </section>`;

  const read = () => {
    const out = { enabled: host.querySelector('#imgdrive-enabled').checked };
    DRIVE_FIELDS.forEach(f => { out[f.name] = host.querySelector(`#imgdrive-${f.name}`).value.trim(); });
    return out;
  };
  const result = host.querySelector('#imgdrive-result');
  const run = async (btn, url, okText) => {
    btn.disabled = true;
    result.textContent = btn.id === 'imgdrive-test' ? 'Testing…' : 'Saving…';
    const r = await apiFetch(url, { method: 'POST', body: JSON.stringify(read()) });
    btn.disabled = false;
    result.textContent = '';
    if (!r) return false;
    if (!r.ok) { await showApiError(r, 'The image drive did not accept the request.', 'Image drive'); return false; }
    showToast(okText, { variant: 'success' });
    return true;
  };
  host.querySelector('#imgdrive-test').addEventListener('click', e =>
    run(e.currentTarget, '/api/v1/admin/image-drive/test', 'Connection works: a test file was written, read back and deleted.'));
  host.querySelector('#imgdrive-save').addEventListener('click', async e => {
    if (await run(e.currentTarget, '/api/v1/admin/image-drive', 'Image drive saved.')) renderImageDriveSection(host);
  });
}
