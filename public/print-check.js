// print-check.js - 251.1 / 251.2: the smart print check.
//
// The first real print on a printer and roll asks how it came out, with
// three faces. Perfect is kept on the Printer (align_confirmed), so each
// printer and roll is asked once. Bit off or Bad opens Fix from photo: the
// ERP prints a check label (a thin frame and a corner dot on every label),
// the user photographs it, print-align.js reads how far the print sits off
// the labels, and Apply saves that on the printer's preset. The check then
// prints again and asks again. A photo with no check frame (a Line up, the
// alignment test, the label's own print) is read from the tags instead
// (257.1): sideways only. Loaded only when a print asks, or from
// Printer preset's Fix from photo.
const Q = new URL(import.meta.url).search;
const A = await import('./print-align.js' + Q);
const E = await import('./sticker-engine.js' + Q);

const esc = s => (typeof window.escapeHTMLText === 'function' ? window.escapeHTMLText(s) : String(s ?? '').replace(/[&<>"']/g, c => `&#${c.charCodeAt(0)};`));
const num = v => Number(v) || 0;
const r1 = v => Math.round(v * 10) / 10;
const mm = v => r1(Number(v));
const PHOTO_MAX_PX = 1600; // the long side the photo is read at
const NO_ROLE = 'Your role cannot change printers';
// saidAs: a preset change in words - "right 1.6 mm", or with the values it
// replaced "right -0.5 → 1.6 mm" (275.1).
const SAID = { shift_right_mm: ['right', ' mm'], shift_down_mm: ['down', ' mm'], skew_deg: ['skew', '°'] };
const saidAs = (changes, was) => Object.entries(changes).map(([k, v]) =>
  `${SAID[k][0]} ${was ? `${mm(num(was[k]))} → ` : ''}${v}${SAID[k][1]}`).join(', ');
const SKEW_MIN_DEG = 0.5;  // a short frame reads about 0.2° of noise

// rollKey names a roll as align_confirmed stores it: one label's size, the
// labels across, v for a vertical print - the same tag roll whatever design.
export const rollKey = t => `${mm(t.w)}x${mm(t.h)}x${t.settings.per_row}${t.settings.direction === 'vertical' ? 'v' : ''}`;

const getKey = (kind, k) => { try { return window[kind].getItem(k) || ''; } catch { return ''; } };
const setKey = (kind, k, v) => { try { window[kind].setItem(k, v); } catch { /* private window: asked again later */ } };
const localKey = p => `erp.printOk.${p.id || p.code}`;
const keysOf = s => String(s || '').split(',').map(x => x.trim()).filter(Boolean);
const confirmed = (p, key) => keysOf(p.align_confirmed).concat(keysOf(getKey('localStorage', localKey(p)))).includes(key);
const mayUpdate = p => !!p.id && (typeof window.canUpdateDoctype !== 'function' || window.canUpdateDoctype('Printer'));

async function savePrinter(p, changes) {
  const { patchDoc } = await import('./sticker-studio.js' + Q);
  if (!await patchDoc('Printer', p.id, changes)) return false;
  Object.assign(p, changes);
  return true;
}

// freshen: the page's copy of the printer can be older than a change saved
// since (another tab or computer); a fix adds to the preset as saved now.
const PRESET_KEYS = ['shift_right_mm', 'shift_down_mm', 'skew_deg', 'darkness', 'print_speed', 'align_confirmed'];
async function freshen(p) {
  if (!p.id) return;
  try {
    const res = await window.apiFetch(`/api/v1/doc/Printer/${encodeURIComponent(p.id)}`);
    if (!res?.ok) return;
    const doc = await res.json();
    for (const k of PRESET_KEYS) p[k] = doc[k] ?? '';
  } catch { /* offline: the page's copy */ }
}

// Perfect: kept on the Printer for every computer; a role that cannot change
// printers (or a save that fails) keeps it on this computer instead, rather
// than asking every day. 275.3: returns where it is kept, 'printer' or
// 'computer', so the answer says which.
async function markPerfect(p, key) {
  const where = () => (keysOf(p.align_confirmed).includes(key) ? 'printer' : 'computer');
  if (confirmed(p, key)) return where();
  if (mayUpdate(p)) await freshen(p);
  if (confirmed(p, key)) return where();
  if (mayUpdate(p) && await savePrinter(p, { align_confirmed: keysOf(p.align_confirmed).concat(key).join(',') })) return 'printer';
  setKey('localStorage', localKey(p), keysOf(getKey('localStorage', localKey(p))).concat(key).join(','));
  return 'computer';
}
const perfectSaid = where => (where === 'printer' ? ['Saved for this roll.', 'success'] : ['Kept on this computer.', 'info']);

// The faces: drawn here, not the system emoji font, so they look the same
// on every computer and phone.
const FACE_BASE = `<circle cx="32" cy="33" r="27" fill="#FFD54A" stroke="#F0A81C" stroke-width="2.5"/>
  <ellipse cx="23" cy="17" rx="7" ry="3.5" fill="#fff" opacity=".45" transform="rotate(-20 23 17)"/>
  <ellipse cx="16.5" cy="41" rx="5" ry="3.2" fill="#FF8FA3" opacity=".6"/><ellipse cx="47.5" cy="41" rx="5" ry="3.2" fill="#FF8FA3" opacity=".6"/>`;
const INK = '#5B3A1A';
const eye = (x) => `<circle cx="${x}" cy="31" r="3.6" fill="${INK}"/><circle cx="${x + 1.2}" cy="29.8" r="1.2" fill="#fff"/>`;
const FACES = [
  { key: 'perfect', label: 'Perfect', cls: 'pc-good', svg: `${FACE_BASE}
    <path d="M17 31q5-6 10 0M37 31q5-6 10 0" stroke="${INK}" stroke-width="3" fill="none" stroke-linecap="round"/>
    <path d="M21 39q11 14 22 0z" fill="${INK}"/><path d="M26.5 45.5q5.5 4 11 0q-5.5-3.5-11 0z" fill="#FF6B81"/>
    <path d="M53 8l1.5 3.5 3.5 1.5-3.5 1.5-1.5 3.5-1.5-3.5-3.5-1.5 3.5-1.5z" fill="#FFC107"/>` },
  { key: 'off', label: 'Bit off', cls: 'pc-ok', svg: `${FACE_BASE}${eye(21)}${eye(41)}
    <path d="M23 45q4.5-3.5 9 0t9 0" stroke="${INK}" stroke-width="3" fill="none" stroke-linecap="round"/>
    <path d="M52 13q4.5 6.5 0 9.5q-4.5-3 0-9.5z" fill="#6EC6FF"/>` },
  { key: 'bad', label: 'Bad', cls: 'pc-bad', svg: `${FACE_BASE}${eye(21)}${eye(41)}
    <path d="M15 23l9 3M49 23l-9 3" stroke="${INK}" stroke-width="2.6" fill="none" stroke-linecap="round"/>
    <path d="M23 49q9-9 18 0" stroke="${INK}" stroke-width="3" fill="none" stroke-linecap="round"/>
    <path d="M17 36q3.5 6 0 8.5q-3.5-2.5 0-8.5z" fill="#6EC6FF"/>` }
];
const facesHTML = () => `<div class="pc-faces">${FACES.map(f =>
  `<button type="button" class="pc-face ${f.cls}" data-face="${f.key}"><svg viewBox="0 0 64 64" aria-hidden="true">${f.svg}</svg><span>${f.label}</span></button>`).join('')}</div>`;

// askAfterPrint: after a real print (sticker-engine.js printStickersNoDialog)
// the first time on this printer and roll. Asked once a session until
// answered; Perfect ends it for good.
let asking = false;
export async function askAfterPrint(p, t) {
  if (!p || !(p.id || p.code) || !t || asking) return;
  const key = rollKey(t);
  const seen = `erp.printAsk.${p.id || p.code}.${key}`;
  if (confirmed(p, key) || getKey('sessionStorage', seen)) return;
  setKey('sessionStorage', seen, '1');
  asking = true;
  await E.stickerStylesReady;
  const bar = document.createElement('div');
  bar.className = 'pc-ask';
  bar.setAttribute('role', 'dialog');
  bar.setAttribute('aria-labelledby', 'pc-ask-title');
  bar.innerHTML = `<div class="pc-ask-head"><span class="pc-ask-title" id="pc-ask-title" title="${esc(p.name || p.code)}">How did it print?</span>
    <button type="button" class="modal-close" aria-label="Close" data-face="close">×</button></div>${facesHTML()}`;
  document.body.appendChild(bar);
  // Toasts sit in the same corner ("Sent to ..." comes with the print): they
  // stack above the bar while it is open.
  document.documentElement.style.setProperty('--pc-ask-h', `${bar.offsetHeight}px`);
  document.body.classList.add('pc-asking');
  requestAnimationFrame(() => bar.classList.add('open'));
  bar.addEventListener('click', async (e) => {
    const face = e.target.closest('[data-face]')?.dataset.face;
    if (!face) return;
    bar.remove();
    document.body.classList.remove('pc-asking');
    asking = false;
    if (face === 'perfect') {
      const [msg, variant] = perfectSaid(await markPerfect(p, key));
      window.showToast?.(`${msg} Won't ask again.`, { variant });
    } else if (face !== 'close') {
      openFixFromPhoto(p, t);
    }
  });
}

// openFixFromPhoto: print the check label, add a photo of it, Apply the fix.
// roll is the template the printer prints this roll with; onSaved runs after
// each saved change.
export async function openFixFromPhoto(p, roll, { onSaved } = {}) {
  await E.stickerStylesReady;
  const plan = await E.checkLabelPlan(roll);
  const t = plan.t, key = rollKey(t);
  if (t.settings.sheet.mode === 'a4') { await window.showCustomAlert?.('Fix from photo is for label rolls.', 'Fix from photo'); return; }
  await freshen(p);
  const lang = String(p.printer_language || '').toUpperCase();
  const canSkew = lang === 'TSPL' || lang === 'ZPL';
  const may = mayUpdate(p);
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `
    <div class="modal-container pc-dialog" role="dialog" aria-modal="true" aria-labelledby="pc-fix-title">
      <div class="modal-header"><h3 class="modal-title" id="pc-fix-title">Fix from photo · ${esc(p.name || p.code)}</h3>
        <button type="button" class="modal-close" aria-label="Close dialog" data-f="close">×</button></div>
      <div class="modal-body">
        <ol class="pc-steps">
          <li><span>Print the check label</span><button type="button" class="btn btn-outline" data-f="print">Print check label</button></li>
          <li><span>Photo it from above</span><label class="btn btn-outline pc-file">Add photo<input type="file" accept="image/*" hidden></label></li>
        </ol>
        <div class="pc-drop" data-f="drop"><span>Drop or paste the photo here</span><canvas class="pc-shot" hidden></canvas></div>
        <div class="pc-saved" aria-live="polite" hidden></div>
        <div class="pc-result" aria-live="polite"></div>
        <div class="pc-again" hidden><span class="pc-again-title">How is it now?</span>${facesHTML()}</div>
        <div class="form-group pc-lineup" hidden><span class="form-label">Best row</span>
          <div class="flex-row-gap">${E.LINE_UP_STEPS_MM.map((s, i) => `<button type="button" class="btn btn-outline" data-pick="${i}">${String.fromCharCode(65 + i)}</button>`).join('')}</div></div>
        <div class="login-error hidden pc-error"></div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" data-f="lineup">Line up</button>
        <button type="button" class="btn btn-outline" data-f="close">Cancel</button>
        <button type="button" class="btn btn-primary" data-f="apply" disabled title="${may ? 'Add a photo first' : NO_ROLE}">Apply</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  requestAnimationFrame(() => overlay.classList.add('open'));
  const $ = s => overlay.querySelector(s);
  const errorEl = $('.pc-error'), resultEl = $('.pc-result'), shot = $('.pc-shot'), drop = $('.pc-drop'), again = $('.pc-again');
  const applyBtn = $('[data-f="apply"]'), lineupEl = $('.pc-lineup'), printBtn = $('[data-f="print"]'), savedEl = $('.pc-saved');
  // 275.2: a greyed-out Apply says why on its tip; 275.1: Apply reads Saved
  // after a save until the next photo is read.
  const setApply = (label, why) => { applyBtn.textContent = label; applyBtn.disabled = !!why; applyBtn.title = why || ''; };
  // 275.1: what was saved stays in the dialog, old value to new, whatever the
  // check label's print does next (a toast alone was gone by then).
  const showSaved = (changes, was) => {
    savedEl.textContent = `Saved · ${saidAs(changes, was)}`;
    savedEl.hidden = false;
    reveal(savedEl);
  };
  // 255.1: what just appeared (the result, an error, "How is it now?") is
  // scrolled into view; on a short window it would sit under the fold.
  const reveal = (el) => requestAnimationFrame(() => el.scrollIntoView({ block: 'nearest', behavior: 'smooth' }));
  const say = (msg) => { errorEl.textContent = msg; errorEl.classList.toggle('hidden', !msg); if (msg) reveal(errorEl); };
  let fix = null, fixText = '', lineRows = null;

  // Escape closes this dialog only when it is the top one (the print preview
  // opens over it).
  const onKey = (e) => { const all = document.querySelectorAll('.modal-overlay'); if (e.key === 'Escape' && all[all.length - 1] === overlay) close(); };
  const onPaste = (e) => { const f = [...(e.clipboardData?.files || [])].find(x => x.type.startsWith('image/')); if (f) { e.preventDefault(); readPhoto(f); } };
  const close = () => { overlay.remove(); document.removeEventListener('keydown', onKey); document.removeEventListener('paste', onPaste); };
  document.addEventListener('keydown', onKey);
  document.addEventListener('paste', onPaste);
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
  drop.addEventListener('dragover', (e) => { e.preventDefault(); drop.classList.add('over'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('over'));
  drop.addEventListener('drop', (e) => {
    e.preventDefault();
    drop.classList.remove('over');
    const f = [...(e.dataTransfer?.files || [])].find(x => x.type.startsWith('image/'));
    if (f) readPhoto(f);
  });
  $('.pc-file input').addEventListener('change', (e) => { const f = e.target.files[0]; e.target.value = ''; if (f) readPhoto(f); });

  // The fix the photo asks for, as the values to save: shift always, skew on
  // a printer the ERP draws the bitmap for, the gap only reported (it belongs
  // to the roll's template, not the printer).
  const changesFor = (res) => {
    const c = { shift_right_mm: r1(num(p.shift_right_mm) + res.right) };
    if (res.down !== null) c.shift_down_mm = r1(num(p.shift_down_mm) + res.down);
    if (canSkew && res.skew !== null && Math.abs(res.skew) >= SKEW_MIN_DEG) c.skew_deg = r1(Math.max(-5, Math.min(5, num(p.skew_deg) + res.skew)));
    return c;
  };

  // 257.1: what could be on the tags of a photo with no check frame, each
  // drawn as it lands on a tag with the shift it was printed with: the Line
  // up rows (as printed in this dialog, else as they would print now), the
  // check label, the alignment test and the label's own print.
  const tagCandidates = () => {
    const o = num(t.settings.offset_x_mm), cur = num(p.shift_right_mm);
    const ink = (tm, id, shift, kind) => Object.assign({ id, kind, shift: o + shift }, E.cellInk(tm, { sku: '', name: '' }, A.TAG_STEP));
    const printing = elements => Object.assign({}, t, { elements });
    const own = () => { try { return roll ? [ink(roll, 'label', cur)] : []; } catch { return []; } };
    return [
      ...(lineRows || E.lineUpRows(p)).map(row => ink(printing(E.lineUpElements(t.w, t.h, row)), row.letter, row.shift_right_mm, 'line')),
      ink(t, 'check', cur),
      ink(printing(E.alignmentTestElements(t.w, t.h, p)), 'test', cur),
      ...own()
    ];
  };
  const readFrom = picks => {
    const ids = [...new Set(picks.map(x => x.id))], rows = ids.filter(id => id.length === 1).sort();
    const names = { check: 'Check label', test: 'Alignment test', label: 'Label print' };
    return `Read from ${(rows.length ? [`Line up ${rows.join(', ')}`] : []).concat(ids.filter(id => names[id]).map(id => names[id])).join(', ')}`;
  };

  async function readPhoto(file) {
    say('');
    fix = null;
    setApply('Apply', may ? 'Reading the photo' : NO_ROLE);
    again.hidden = true;
    resultEl.innerHTML = '<span class="pc-reading">Reading the photo…</span>';
    let img, ctx;
    try {
      const bmp = await createImageBitmap(file, { imageOrientation: 'from-image' });
      const k = Math.min(1, PHOTO_MAX_PX / Math.max(bmp.width, bmp.height));
      shot.width = Math.round(bmp.width * k);
      shot.height = Math.round(bmp.height * k);
      ctx = shot.getContext('2d', { willReadFrequently: true });
      ctx.drawImage(bmp, 0, 0, shot.width, shot.height);
      if (bmp.close) bmp.close();
      img = ctx.getImageData(0, 0, shot.width, shot.height);
    } catch {
      resultEl.innerHTML = '';
      say('Could not open that photo. Use a JPG or PNG.');
      return;
    }
    shot.hidden = false;
    drop.classList.add('has-shot');
    await new Promise(r => setTimeout(r, 30)); // let "Reading" paint
    let res = A.detectAlignment(img, plan.geo);
    if (!res.ok && res.why === 'frame') {
      const cell = E.cellSize(t), r = A.readTags(img, { w: cell.w, pitch: cell.w + num(t.settings.gap_h_mm), perRow: t.settings.per_row }, tagCandidates());
      res = r.ok ? { ok: true, right: r.shift - num(p.shift_right_mm), down: null, skew: null, gap: r.gap, labels: r.labels, picks: r.picks } : r;
    }
    drawFound(ctx, res);
    if (!res.ok) {
      resultEl.innerHTML = '';
      setApply('Apply', may ? 'Photo not read' : NO_ROLE);
      say({
        tags: 'No labels found. Photo the roll from above, on a dark table.',
        print: 'Print not known. Photo a check label, Line up or alignment test.',
        edges: 'Label edges not clear. Put a dark sheet behind the roll and photo again.',
        size: `The label reads ${res.width} mm wide; the roll says ${mm(plan.geo.w)} mm. Check the roll set on this printer.`
      }[res.why] || 'Could not read the photo.');
      return;
    }
    fix = changesFor(res);
    const notes = [];
    if (res.skew !== null && Math.abs(res.skew) >= SKEW_MIN_DEG) notes.push(canSkew ? `Slant ${Math.abs(res.skew).toFixed(1)}° fixed too` : `Slant ${Math.abs(res.skew).toFixed(1)}°, not fixable here`);
    if (res.gap !== null) notes.push(`Gap across is ${mm(t.settings.gap_h_mm + res.gap)} mm on the roll, ${mm(t.settings.gap_h_mm)} mm in the template`);
    if (res.picks) notes.push(readFrom(res.picks));
    if (res.down === null) notes.push('Up and down not read');
    const same = Math.abs(res.right) < 0.25 && (res.down === null || Math.abs(res.down) < 0.25) && !fix.skew_deg;
    fixText = saidAs(fix);
    resultEl.innerHTML = `<div class="pc-advice${same ? ' ok' : ''}">${esc(A.adviceText(res))}</div>
      <div class="pc-values">New preset: ${esc(fixText)}</div>
      ${notes.map(n => `<div class="pc-note">${esc(n)}</div>`).join('')}`;
    setApply('Apply', !may ? NO_ROLE : same ? 'Already centred' : '');
    if (!may) say('Your role cannot change printers. Ask a manager to apply this.');
    if (same) again.hidden = false;
    reveal(same ? again : resultEl);
  }

  // The photo with what was read: the print's frame green where read, amber
  // where not, and the label's edges blue.
  function drawFound(ctx, res) {
    const lw = Math.max(2, shot.width / 350);
    ctx.lineWidth = lw;
    for (const l of res.labels || []) {
      ctx.strokeStyle = l.found ? '#16a34a' : '#f59e0b';
      ctx.beginPath();
      (l.outline || []).forEach(([x, y], i) => (i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)));
      ctx.closePath();
      ctx.stroke();
      if (!l.found || !l.map) continue;
      const g = plan.geo, x0 = l.right, x1 = g.w + l.right;
      const y0 = l.down !== undefined ? l.down : g.rect.y0 - 2, y1 = l.down !== undefined ? g.h + l.down : g.rect.y1 + 2;
      ctx.strokeStyle = '#2563eb';
      ctx.setLineDash([lw * 3, lw * 2]);
      ctx.beginPath();
      [[x0, y0], [x1, y0], [x1, y1], [x0, y1]].map(([x, y]) => l.map(x, y)).forEach(([x, y], i) => (i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)));
      ctx.closePath();
      ctx.stroke();
      ctx.setLineDash([]);
    }
  }

  // 275.2: Print check label reads Printing… while its print runs, whoever
  // started it (the button, Apply, a Line up row).
  const printCheck = async () => {
    lineupEl.hidden = true;
    printBtn.disabled = true;
    printBtn.textContent = 'Printing…';
    try {
      if (await E.printCheckLabel(plan, p)) { again.hidden = false; reveal(again); }
    } finally {
      printBtn.disabled = false;
      printBtn.textContent = 'Print check label';
    }
  };

  overlay.addEventListener('click', async (e) => {
    const pick = e.target.closest('[data-pick]');
    const face = e.target.closest('[data-face]')?.dataset.face;
    const act = pick ? 'pick' : face ? 'face' : e.target.closest('[data-f]')?.dataset.f;
    if (!act) return;
    if (act === 'drop') { $('.pc-file input').click(); return; }
    if (act === 'close') { close(); return; }
    if (act === 'face') {
      if (face === 'perfect') {
        const [msg, variant] = perfectSaid(await markPerfect(p, key));
        close();
        window.showToast?.(`Lovely. ${msg}`, { variant });
      } else {
        again.hidden = true;
        resultEl.innerHTML = '';
        shot.hidden = true;
        drop.classList.remove('has-shot');
        say('Photo the new check label.');
      }
      return;
    }
    const btn = e.target.closest('button');
    if (btn) btn.disabled = true;
    say('');
    try {
      if (act === 'print') {
        await printCheck();
      } else if (act === 'apply' && fix) {
        if (Math.abs(fix.shift_right_mm) > 20 || Math.abs(fix.shift_down_mm || 0) > 20) { say('That is more than 20 mm. Check the roll set on this printer.'); return; }
        const was = { ...p };
        if (!await savePrinter(p, fix)) return;
        onSaved?.(p);
        showSaved(fix, was);
        fix = null;
        setApply('Saved', 'Saved');
        resultEl.innerHTML = '';
        shot.hidden = true;
        drop.classList.remove('has-shot');
        await printCheck();
      } else if (act === 'lineup') {
        lineRows = await E.printLineUp(t, p);
        lineupEl.hidden = !lineRows;
        if (lineRows) lineupEl.querySelectorAll('[data-pick]').forEach((b, i) => { b.title = `Shift right ${lineRows[i].shift_right_mm} mm`; });
      } else if (act === 'pick' && lineRows) {
        if (!may) { say('Your role cannot change printers.'); return; }
        const row = lineRows[Number(pick.dataset.pick)], was = { ...p };
        if (!await savePrinter(p, { shift_right_mm: row.shift_right_mm })) return;
        onSaved?.(p);
        showSaved({ shift_right_mm: row.shift_right_mm }, was);
        await printCheck();
      }
    } catch (err) {
      say(err.message || String(err));
    } finally {
      // Apply keeps the state setApply gave it (Saved, or Apply again when
      // the save did not go through); the other buttons come back.
      if (btn === applyBtn) { if (fix) setApply('Apply', may ? '' : NO_ROLE); } else if (btn && btn !== printBtn) btn.disabled = false;
    }
  });
}
