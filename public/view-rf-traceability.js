// BLD-041: native ES module for RF traceability tasks.
// Loaded only after the existing route-entitlement check succeeds.

// --- RF traceability task shell (Stage 47.6.2/47.6.3/47.6.4/47.6.7) --------
//
// 47.6.7 named the concrete gap: four real, tested, registered endpoints with
// ZERO caller anywhere in this file, so the operations that make a lot usable
// were reachable only by a developer with curl.
//
//   POST /api/v1/wms/batch/putaway       bin-level lot assignment - what makes
//                                        a lot visible to FEFO at all
//   POST /api/v1/wms/batch/consume       issue a specific lot out of a bin
//   POST /api/v1/wms/batch/expiry-sweep  quarantine everything past its date
//   POST /api/v1/wms/serial/status       allocate / ship / return / scrap
//
// 47.6.2 said to build them as part of the RF task shell rather than a sixth
// desktop WMS screen, because they are floor operations. So this is that
// shell, and it is deliberately NOT the desktop layout at a narrow width:
//
//   - no sidebar (the shell hides it while a task is open),
//   - ONE instruction at the top, in words, at a size readable at arm's length,
//   - ONE primary scan target that keeps focus and re-takes it after every
//     action, so an operator never has to tap an input between scans,
//   - a human label beside every code, and no naked UUID anywhere,
//   - distinct sound + vibration + colour + TEXT per outcome (47.6.4) - never
//     colour alone, which is unreadable to a colour-blind operator and
//     invisible in sunlight,
//   - every control at least 44x44 CSS px, and the layout is a single column
//     that reflows rather than scrolling sideways (47.6.3).

const RF_TASKS = [
  {
    key: 'putaway',
    label: 'Put a lot away',
    instruction: 'Scan the bin, then the item, then the lot.',
    endpoint: '/api/v1/wms/batch/putaway',
    fields: [
      { id: 'rf-putaway-bin', label: 'Bin', semantic: 'barcode', body: 'bin_code', required: true },
      { id: 'rf-putaway-sku', label: 'Item', semantic: 'barcode', body: 'sku', required: true },
      { id: 'rf-putaway-lot', label: 'Lot / Batch', semantic: 'lot', body: 'batch_no', required: true },
      { id: 'rf-putaway-qty', label: 'Quantity', semantic: 'number', body: 'qty', required: true, type: 'number' },
      { id: 'rf-putaway-condition', label: 'Condition', semantic: 'text', body: 'condition', options: ['Good', 'Damaged', 'QC-Hold', 'RTV'] }
    ],
    done: 'Lot put away. It can now be picked.'
  },
  {
    key: 'consume',
    label: 'Issue a lot',
    instruction: 'Scan the bin, the item and the lot you are taking stock from.',
    endpoint: '/api/v1/wms/batch/consume',
    fields: [
      { id: 'rf-consume-bin', label: 'Bin', semantic: 'barcode', body: 'bin_code', required: true },
      { id: 'rf-consume-sku', label: 'Item', semantic: 'barcode', body: 'sku', required: true },
      { id: 'rf-consume-lot', label: 'Lot / Batch', semantic: 'lot', body: 'batch_no', required: true },
      { id: 'rf-consume-qty', label: 'Quantity', semantic: 'number', body: 'qty', required: true, type: 'number' },
      { id: 'rf-consume-doc', label: 'Against document (optional)', semantic: 'text', body: 'voucher_id' },
      { id: 'rf-consume-customer', label: 'For customer (optional)', semantic: 'text', body: 'customer' }
    ],
    done: 'Lot issued.'
  },
  {
    key: 'serial',
    label: 'Change a serial',
    instruction: 'Scan the item and the serial, then choose what happened to it.',
    endpoint: '/api/v1/wms/serial/status',
    fields: [
      { id: 'rf-serial-sku', label: 'Item', semantic: 'barcode', body: 'sku', required: true },
      { id: 'rf-serial-no', label: 'Serial number', semantic: 'serial', body: 'serial_no', required: true },
      { id: 'rf-serial-status', label: 'New status', semantic: 'text', body: 'status', required: true,
        options: ['In Stock', 'Allocated', 'Shipped', 'Returned', 'Scrapped'] },
      { id: 'rf-serial-reason', label: 'Reason (required to scrap)', semantic: 'text', body: 'reason' }
    ],
    done: 'Serial updated.'
  },
  {
    key: 'expiry',
    label: 'Sweep expired lots',
    instruction: 'This quarantines every lot that is past its expiry date. Nothing else changes.',
    endpoint: '/api/v1/wms/batch/expiry-sweep',
    fields: [],
    confirm: 'Quarantine every lot that is past its expiry date?',
    done: 'Expiry sweep finished.'
  }
];

let rfActiveTask = null;

async function renderRFTraceabilityView(container) {
  const wrap = document.createElement('div');
  wrap.className = 'rf-wrap';
  wrap.innerHTML = `
    <div class="rf-header">
      <div class="rf-title" id="rf-title">Choose a task</div>
      <div class="rf-instruction" id="rf-instruction">Pick what you are doing. Everything after that is one scan at a time.</div>
      <button class="rf-help-btn" type="button" aria-label="Help for RF Traceability" title="Help for this screen" ${actionAttrs('openHelpDrawer', ['rf-traceability'])}>Help</button>
    </div>
    <div id="rf-body"></div>
    <div class="rf-status" id="rf-status" role="status" aria-live="assertive"></div>
  `;
  container.appendChild(wrap);
  renderRFTaskMenu();
}

function renderRFTaskMenu() {
  rfActiveTask = null;
  const body = document.getElementById('rf-body');
  if (!body) return;
  document.getElementById('rf-title').textContent = 'Choose a task';
  document.getElementById('rf-instruction').textContent = 'Pick what you are doing. Everything after that is one scan at a time.';
  setRFStatus('', '');
  body.innerHTML = RF_TASKS.map(t => `
    <button class="rf-task-btn" type="button" ${actionAttrs('openRFTask', [t.key])}>
      <span class="rf-task-label">${cfgEsc(t.label)}</span>
      <span class="rf-task-hint">${cfgEsc(t.instruction)}</span>
    </button>`).join('');
}

function openRFTask(key) {
  const task = RF_TASKS.find(t => t.key === key);
  if (!task) return;
  rfActiveTask = task;
  const body = document.getElementById('rf-body');
  document.getElementById('rf-title').textContent = task.label;
  document.getElementById('rf-instruction').textContent = task.instruction;
  setRFStatus('', '');

  const fields = task.fields.map(f => {
    // Ids are named so the 47.6.1 semantic registry resolves them (rf-*-lot,
    // rf-*-serial, rf-*-bin), which is what keeps the shared keystroke filter
    // away from them - a lot or serial legitimately contains letters, and
    // stripping them silently is exactly the bug that item exists to kill.
    if (f.options) {
      return `
        <label class="rf-field">
          <span class="rf-label">${cfgEsc(f.label)}</span>
          <select id="${f.id}" class="rf-input">
            ${f.options.map(o => `<option value="${cfgEsc(o)}">${cfgEsc(o)}</option>`).join('')}
          </select>
        </label>`;
    }
    return `
      <label class="rf-field">
        <span class="rf-label">${cfgEsc(f.label)}</span>
        <input type="${f.type || 'text'}" id="${f.id}" class="rf-input"
               inputmode="${f.type === 'number' ? 'numeric' : 'text'}"
               autocomplete="off" autocapitalize="characters" spellcheck="false"
               data-rf-semantic="${cfgEsc(f.semantic || 'text')}">
      </label>`;
  }).join('');

  body.innerHTML = `
    <form id="rf-form" autocomplete="off">${fields}
      <button class="rf-primary" type="submit" id="rf-submit">${cfgEsc(task.label)}</button>
      <button class="rf-secondary" type="button" ${actionAttrs('renderRFTaskMenu')}>Back to tasks</button>
      <button class="rf-secondary" type="button" ${actionAttrs('requestRFSupervisor')}>Get a supervisor</button>
    </form>`;

  document.getElementById('rf-form').addEventListener('submit', (e) => {
    e.preventDefault();
    submitRFTask();
  });
  // Scanner focus: the first empty field takes it, and an Enter (which is what
  // every hardware scanner sends after a barcode) advances to the next one
  // rather than submitting a half-filled form.
  task.fields.forEach((f, i) => {
    const el = document.getElementById(f.id);
    if (!el || el.tagName !== 'INPUT') return;
    el.addEventListener('keydown', (e) => {
      if (e.key !== 'Enter') return;
      e.preventDefault();
      const next = task.fields.slice(i + 1).map(n => document.getElementById(n.id)).find(n => n && !n.value);
      if (next) { next.focus(); return; }
      submitRFTask();
    });
  });
  focusFirstEmptyRFField();
}

function focusFirstEmptyRFField() {
  if (!rfActiveTask) return;
  for (const f of rfActiveTask.fields) {
    const el = document.getElementById(f.id);
    if (el && el.tagName === 'INPUT' && !el.value) { el.focus(); return; }
  }
  const submit = document.getElementById('rf-submit');
  if (submit) submit.focus();
}

// setRFStatus is the whole 47.6.4 feedback rule in one place: text ALWAYS,
// plus colour, plus a distinct tone and vibration pattern per outcome. Never
// colour alone - it is unreadable to a colour-blind operator and washes out in
// sunlight - and never sound alone, which is inaudible in a loud aisle.
// RF_OUTCOMES is the whole 47.6.4 feedback vocabulary, in one place.
//
// The item names six outcomes, and they are six because an operator has to be
// able to tell them apart WITHOUT reading: "already scanned" and "wrong item"
// call for completely different next actions, and a single error buzz makes
// the operator stop and read every time. Each carries a distinct tone, a
// distinct vibration rhythm, a distinct colour AND text - four channels,
// because an aisle is loud (sound alone fails), bright (colour alone fails),
// and an operator may be colour-blind (colour alone fails again).
//
// Frequencies rise for good outcomes and fall for bad ones, which is the
// convention every warehouse scanner already trains people on; the two
// "stop and check" outcomes (wrong item, owner mismatch) share the low
// register but differ in rhythm.
const RF_OUTCOMES = {
  ok:             { vibrate: [60],                 tone: 880, ms: 120 },
  duplicate:      { vibrate: [40, 60, 40],         tone: 660, ms: 90 },
  // "Wrong item" is the one an operator must never mistake for a duplicate:
  // a duplicate means "you already did this", a wrong item means "the thing
  // in your hand is not the thing on the screen".
  wrong_item:     { vibrate: [250, 100, 250],      tone: 200, ms: 300 },
  // Owner mismatch (3PL) is a wrong item with a different remedy - the stock
  // is real and correct, it just belongs to somebody else.
  owner_mismatch: { vibrate: [120, 80, 120, 80, 120], tone: 260, ms: 200 },
  // A hold is not a failure: the goods are fine and the operator is being
  // told to stop, so it gets its own mid tone rather than the error buzz.
  hold:           { vibrate: [400],                tone: 330, ms: 400 },
  error:          { vibrate: [200, 80, 200],       tone: 220, ms: 260 },
  offline:        { vibrate: [30, 40, 30, 40, 30], tone: 440, ms: 120 }
};

// rfOutcomeForResponse maps a server refusal onto the outcome vocabulary.
//
// Keyed on the message catalog CODE rather than on the HTTP status, because
// the status cannot tell these apart - INVENT-0104 (blocked stock),
// INVENT-0106 (batch expired) and INVENT-0115 (serial/batch mismatch) are all
// 422, and they call for three different reactions on the floor. The codes are
// the whole reason engines/traceability.go returns precisely-coded
// ValidationErrors rather than plain errors, and this is the first caller that
// actually uses that distinction for anything.
function rfOutcomeForResponse(res, payload) {
  const code = (payload && payload.code) || '';
  switch (code) {
    // The lot or serial in the operator's hand is not the one the system
    // expected, or cannot be issued at all.
    case 'INVENT-0115': // serial/batch mismatch
    case 'INVENT-0102': // stock not available for barcode
      return 'wrong_item';
    // Stock that is real and correct but not available to this movement.
    case 'INVENT-0104': // blocked stock selected
    case 'INVENT-0106': // batch expired
    case 'INVENT-0114': // reserved stock blocked
      return 'hold';
    // 3PL ownership. Named here even though owner enforcement itself is
    // Stage 47.5's work, so the shell already speaks the outcome the moment
    // the server starts returning it, rather than reporting it as a generic
    // error until somebody remembers to come back.
    case 'INVENT-0116':
      return 'owner_mismatch';
    case 'INVENT-0103': // barcode already consumed
      return 'duplicate';
  }
  if (res.status === 409) return 'duplicate';
  return 'error';
}

function setRFStatus(kind, message) {
  const el = document.getElementById('rf-status');
  if (!el) return;
  el.className = 'rf-status' + (kind ? ' rf-status-' + kind : '');
  el.textContent = message || '';
  if (!kind) return;
  const p = RF_OUTCOMES[kind];
  if (!p) return;
  try { if (navigator.vibrate) navigator.vibrate(p.vibrate); } catch (e) { /* unsupported device */ }
  rfTone(p.tone, p.ms);
}

// rfTone is a short WebAudio beep. Deliberately not an audio file: a file is a
// network fetch that fails exactly when the device is on weak Wi-Fi, which is
// the moment the operator most needs the feedback.
let rfAudioCtx = null;
function rfTone(frequency, ms) {
  try {
    const Ctx = window.AudioContext || window.webkitAudioContext;
    if (!Ctx) return;
    rfAudioCtx = rfAudioCtx || new Ctx();
    const osc = rfAudioCtx.createOscillator();
    const gain = rfAudioCtx.createGain();
    osc.frequency.value = frequency;
    gain.gain.value = 0.08;
    osc.connect(gain).connect(rfAudioCtx.destination);
    osc.start();
    setTimeout(() => { try { osc.stop(); } catch (e) { /* already stopped */ } }, ms);
  } catch (e) {
    // No audio available - the text and vibration still carry the outcome.
  }
}

async function submitRFTask() {
  if (!rfActiveTask) return;
  const task = rfActiveTask;
  const body = {};
  for (const f of task.fields) {
    const el = document.getElementById(f.id);
    if (!el) continue;
    const raw = String(el.value || '').trim();
    if (f.required && !raw) {
      setRFStatus('error', `${f.label} is needed before this can be sent.`);
      el.focus();
      return;
    }
    if (!raw) continue;
    body[f.body] = f.type === 'number' ? (parseInt(raw, 10) || 0) : raw;
  }
  if (task.confirm && !(await showCustomConfirm(task.confirm, task.label))) return;

  const submit = document.getElementById('rf-submit');
  if (submit) submit.disabled = true;
  try {
    let res;
    try {
      res = await apiFetch(task.endpoint, { method: 'POST', body: JSON.stringify(body) });
    } catch (e) {
      // 47.6.5: an offline RF action is NOT queued. These four all mutate
      // stock against server-side state (expiry gates, FEFO ordering, serial
      // status transitions) that the device cannot evaluate, so replaying one
      // later could put stock somewhere the server would have refused. The
      // honest behaviour is to say so and let the operator retry when the link
      // is back - the POS offline queue exists because a sale has already
      // physically happened; a putaway has not.
      setRFStatus('offline', 'No connection. Nothing was sent. Try again when the signal comes back.');
      return;
    }
    if (!res) return;
    if (!res.ok) {
      const payload = await res.clone().json().catch(() => ({}));
      const message = await getErrorMessage(res, 'That could not be done.');
      setRFStatus(rfOutcomeForResponse(res, payload), message);
      return;
    }
    const data = await res.json().catch(() => ({}));
    setRFStatus('ok', rfSuccessMessage(task, data));
    // Clear the scan fields but keep the operator on the task - the next unit
    // is the overwhelmingly common next action, and making them re-choose the
    // task every time is how an RF screen gets abandoned.
    task.fields.forEach(f => {
      const el = document.getElementById(f.id);
      if (el && el.tagName === 'INPUT') el.value = '';
    });
    focusFirstEmptyRFField();
  } finally {
    if (submit) submit.disabled = false;
  }
}

function rfSuccessMessage(task, data) {
  if (task.key === 'expiry') {
    const n = Number(data.quarantined ?? data.swept ?? 0);
    return n > 0 ? `${task.done} ${n} lot(s) quarantined.` : `${task.done} Nothing was past its date.`;
  }
  return task.done;
}

// requestRFSupervisor is 47.6.2's "one recovery/supervisor action reachable
// without sharing the operator's own credentials". It raises a request the
// supervisor answers on their OWN device - it never asks the operator to hand
// their phone over, and never asks a supervisor to type their password into
// somebody else's session, which is how shared logins start.
async function requestRFSupervisor() {
  const reason = await showCustomPrompt(
    'What do you need help with? A supervisor will see this on their own device.',
    '', 'Get a Supervisor');
  if (!reason || !reason.trim()) return;
  // FloorAssistRequest, not Grievance: a grievance is an HR complaint, and
  // filing "I cannot scan this bin" into somebody's HR record would be both
  // wrong and quietly harmful. This is its own small doctype a supervisor sees
  // on the ordinary document screen, on their own device.
  const res = await apiFetch('/api/v1/doc/FloorAssistRequest', {
    method: 'POST',
    body: JSON.stringify({
      task: (rfActiveTask && rfActiveTask.label) || 'Unspecified',
      request: reason.trim(),
      status: 'Open'
    })
  });
  if (res && res.ok) {
    setRFStatus('ok', 'A supervisor has been asked to come to you.');
  } else {
    setRFStatus('error', 'The request could not be sent. Ask a colleague to call a supervisor.');
  }
}

export { renderRFTraceabilityView, renderRFTaskMenu, openRFTask, focusFirstEmptyRFField, rfOutcomeForResponse, setRFStatus, rfTone, submitRFTask, rfSuccessMessage, requestRFSupervisor };
