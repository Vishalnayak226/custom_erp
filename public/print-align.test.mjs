// 251.1: the photo alignment detector against made-up photos of a check
// label printed off by a known amount - taken at an angle, blurred, noisy,
// unevenly lit, upside down - on a plain label roll and on the owner's
// Seznik tail tags. Run: node --test public/print-align.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { checkGeometry, checkElements, contentBox, detectAlignment, homography, adviceText, readTags, TAG_STEP } from './print-align.js';

// scene: what the paper looks like at (x, y) mm. Labels sit where the roll
// really has them (gapA across); the print lands at the template's places
// (gapT across) moved by err and slanted by skewDeg.
function scene({ W, H, across = 1, gapA = 0, gapT = 0, gapV = 3, rows = [0], tail = null, err = { x: 0, y: 0 }, skewDeg = 0, geo, print = true, labelW = W }) {
  const k = Math.tan(skewDeg * Math.PI / 180), yc = H / 2;
  const marks = [];
  if (print) {
    for (let i = 0; i < across; i++) {
      const ox = i * (W + gapT) + err.x, oy = err.y;
      const r = geo.rect, s = geo.stroke, d = geo.dot;
      marks.push({ x0: ox + r.x0, y0: oy + r.y0, x1: ox + r.x1, y1: oy + r.y1, hollow: s });
      marks.push({ x0: ox + d.x, y0: oy + d.y, x1: ox + d.x + d.s, y1: oy + d.y + d.s });
    }
  }
  const inLabel = (x, y) => {
    for (const j of rows) {
      const ly = y - j * (H + gapV);
      if (ly < 0 || ly > H) continue;
      for (let i = 0; i < across; i++) {
        const lx = x - i * (labelW + gapA);
        if (lx < 0 || lx > labelW) continue;
        if (!tail || ly <= tail.from) return true;
        if (Math.abs(lx - labelW / 2) <= tail.w / 2) return true;
      }
    }
    return false;
  };
  const inMark = (x, y) => {
    const px = x - k * (y - yc); // undo the print's slant
    for (const m of marks) {
      if (px < m.x0 || px > m.x1 || y < m.y0 || y > m.y1) continue;
      if (!m.hollow) return true;
      if (px - m.x0 < m.hollow || m.x1 - px < m.hollow || y - m.y0 < m.hollow || m.y1 - y < m.hollow) return true;
    }
    return false;
  };
  const rollW = across * labelW + (across - 1) * gapA;
  return (x, y) => {
    if (x < -2.5 || x > rollW + 2.5) return 95 + 20 * Math.sin(x * 0.7) * Math.cos(y * 0.5);
    if (inMark(x, y) && inLabel(x, y)) return 38;
    return inLabel(x, y) ? 238 : 196;
  };
}

// photo: the paper area [x0..x1] × [y0..y1] mm seen through a camera at an
// angle, w × h pixels; turn: 0 or 2 (upside down).
function photo(paper, area, w, h, { turn = 0, seed = 7, blur = 2 } = {}) {
  const quad = [[0.06 * w, 0.04 * h], [0.95 * w, 0.08 * h], [0.9 * w, 0.97 * h], [0.03 * w, 0.93 * h]];
  const q = turn ? [quad[2], quad[3], quad[0], quad[1]] : quad;
  const toPaper = homography(q, [[area.x0, area.y0], [area.x1, area.y0], [area.x1, area.y1], [area.x0, area.y1]]);
  let g = new Float32Array(w * h);
  for (let v = 0; v < h; v++) for (let u = 0; u < w; u++) {
    let s = 0;
    for (const [a, b] of [[0.25, 0.25], [0.75, 0.25], [0.25, 0.75], [0.75, 0.75]]) { const p = toPaper(u + a, v + b); s += paper(p[0], p[1]); }
    g[v * w + u] = s / 4;
  }
  for (let pass = 0; pass < blur; pass++) {
    const o = new Float32Array(w * h);
    for (let v = 0; v < h; v++) for (let u = 0; u < w; u++) {
      let s = 0, n = 0;
      for (let dv = -1; dv <= 1; dv++) for (let du = -1; du <= 1; du++) {
        const x = u + du, y = v + dv;
        if (x >= 0 && y >= 0 && x < w && y < h) { s += g[y * w + x]; n++; }
      }
      o[v * w + u] = s / n;
    }
    g = o;
  }
  let r = seed;
  const rnd = () => ((r = (r * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff);
  const data = new Uint8ClampedArray(w * h * 4);
  for (let v = 0; v < h; v++) for (let u = 0; u < w; u++) {
    const light = 0.82 + 0.2 * (u / w) + 0.06 * (v / h);  // uneven light
    const val = g[v * w + u] * light + (rnd() - 0.5) * 14;
    const i = (v * w + u) * 4;
    data[i] = val * 1.02; data[i + 1] = val; data[i + 2] = val * 0.94; data[i + 3] = 255;
  }
  return { width: w, height: h, data };
}

const brief = res => JSON.stringify(Object.assign({}, res, { labels: (res.labels || []).map(({ map, outline, ...l }) => l) }));
const near = (got, want, tol, what) => assert.ok(Math.abs(got - want) <= tol, `${what}: got ${got}, want ${want} ± ${tol}`);

test('plain 50 × 25 label: shift right and down', () => {
  const geo = checkGeometry(50, 25);
  const img = photo(scene({ W: 50, H: 25, rows: [-1, 0, 1], err: { x: -1.3, y: 0.8 }, geo }), { x0: -6, y0: -6, x1: 56, y1: 31 }, 1300, 800);
  const res = detectAlignment(img, geo);
  assert.equal(res.ok, true, brief(res));
  near(res.right, 1.3, 0.15, 'right');
  near(res.down, -0.8, 0.15, 'down');
  near(res.skew, 0, 0.2, 'skew');
  assert.equal(adviceText(res), 'Move 1.3 mm right, 0.8 mm up');
});

test('photo taken upside down reads the same', () => {
  const geo = checkGeometry(50, 25);
  const img = photo(scene({ W: 50, H: 25, rows: [-1, 0, 1], err: { x: 0.9, y: -0.6 }, geo }), { x0: -6, y0: -6, x1: 56, y1: 31 }, 1300, 800, { turn: 2, seed: 3 });
  const res = detectAlignment(img, geo);
  assert.equal(res.ok, true, brief(res));
  near(res.right, -0.9, 0.15, 'right');
  near(res.down, 0.6, 0.15, 'down');
});

test('a slanted print is read as skew', () => {
  const geo = checkGeometry(50, 25);
  const img = photo(scene({ W: 50, H: 25, rows: [0], err: { x: 0.5, y: 0 }, skewDeg: 1.5, geo }), { x0: -6, y0: -6, x1: 56, y1: 31 }, 1300, 800, { seed: 11 });
  const res = detectAlignment(img, geo);
  assert.equal(res.ok, true, brief(res));
  near(res.skew, -1.5, 0.3, 'skew');
  near(res.right, -0.5, 0.2, 'right');
});

// The owner's roll: Seznik 100 × 14.3 tail tags, three across, printed
// vertical. The head is full width for 65 mm, then a 4 mm tail.
const SEZNIK = { W: 14.3, H: 100, across: 3, tail: { from: 65, w: 4 } };
const seznikGeo = () => {
  const box = contentBox([{ x_mm: 4.7, y_mm: 0.5, w_mm: 57.4, h_mm: 13.3 }], true, 14.3);
  return checkGeometry(14.3, 100, box);
};

test('Seznik tail tags: frame lands on the head, shift found from all three', () => {
  const geo = seznikGeo();
  assert.ok(geo.rect.y1 < 65 && geo.rect.y0 > 2, `frame on the head: ${JSON.stringify(geo.rect)}`);
  geo.pitch = 14.3 + 3.2;
  const img = photo(scene(Object.assign({}, SEZNIK, { gapA: 3.2, gapT: 3.2, rows: [0, 1], err: { x: -2.0, y: 0.5 }, geo })), { x0: -4, y0: -6, x1: 53, y1: 104 }, 820, 1600, { seed: 5 });
  const res = detectAlignment(img, geo);
  assert.equal(res.ok, true, brief(res));
  assert.equal(res.labels.filter(l => l.found).length, 3);
  near(res.right, 2.0, 0.15, 'right');
  near(res.down, -0.5, 0.2, 'down (from the head end)');
  assert.equal(res.gap, null);
  assert.ok(res.drift < 0.3, `drift ${res.drift}`);
});

test('Seznik with the wrong gap across: the gap is found, not hidden in the shift', () => {
  const geo = seznikGeo();
  geo.pitch = 14.3 + 2;
  const img = photo(scene(Object.assign({}, SEZNIK, { gapA: 3.2, gapT: 2, rows: [0], err: { x: -1.0, y: 0 }, geo })), { x0: -4, y0: -6, x1: 53, y1: 104 }, 820, 1600, { seed: 9 });
  const res = detectAlignment(img, geo);
  assert.equal(res.ok, true, brief(res));
  near(res.gap, 1.2, 0.15, 'gap');
  near(res.gapRight, 1.0, 0.15, 'shift with the gap fixed');
  near(res.right, 2.2, 0.2, 'shift without it (middle label)');
});

test('no check label in the photo', () => {
  const geo = checkGeometry(50, 25);
  const img = photo(scene({ W: 50, H: 25, rows: [0], geo, print: false }), { x0: -6, y0: -6, x1: 56, y1: 31 }, 900, 560);
  assert.equal(detectAlignment(img, geo).why, 'frame');
});

test('a roll narrower than the template says is caught', () => {
  const geo = checkGeometry(50, 25);
  const img = photo(scene({ W: 50, H: 25, labelW: 40, rows: [0], geo }), { x0: -6, y0: -6, x1: 56, y1: 31 }, 1300, 800);
  const res = detectAlignment(img, geo);
  assert.equal(res.ok, false);
  assert.equal(res.why, 'size');
});

test('check elements turn with a vertical template', () => {
  const geo = checkGeometry(14.3, 100);
  const [frame, dot] = checkElements(geo, true, 14.3);
  // Cell (x, y) is design (y, 14.3 - x): the frame's cell x0 becomes its far design edge.
  near(frame.x_mm, geo.rect.y0, 1e-9, 'frame x');
  near(frame.y_mm + frame.h_mm, 14.3 - geo.rect.x0, 1e-9, 'frame bottom');
  near(dot.x_mm, geo.dot.y, 1e-9, 'dot x');
  assert.equal(dot.fill, true);
});

// ------------------------------------------------------- no check frame ---
// 257.1: a Line up photographed with no check frame. Rows along the roll
// carry their own letter and were printed with their own shift; the printer
// adds the same error to all of them. readTags must find the tags, tell the
// rows apart and give back the shift that cancels the error.

// lineUpInk: a Line up tag W × H mm (cell frame): box 0.5 mm in, the row's
// letter in the middle (bars that differ by row) and the "R x" text top left.
function lineUpInk(k, W, H) {
  const lh = Math.min(12, H * 0.7), cx = W / 2, cy = H / 2;
  const bars = [[0, 1], [0, 2], [1, 2], [0, 1, 2], [1]][k];
  return (x, y) => {
    if (x >= 0.5 && x <= W - 0.5 && y >= 0.5 && y <= H - 0.5 &&
      (x - 0.5 < 0.3 || W - 0.5 - x < 0.3 || y - 0.5 < 0.3 || H - 0.5 - y < 0.3)) return true;
    if (Math.abs(y - cy) <= lh / 2 && x >= cx - 3 && x <= cx - 1.8) return true;
    for (const b of bars) { const by = cy - lh / 2 + b * (lh - 1.2) / 2; if (y >= by && y <= by + 1.2 && x >= cx - 3 && x <= cx + 3) return true; }
    for (const bx of [1.2, 3, 4.8]) if (x >= bx && x <= bx + 1.2 && y >= 1.2 && y <= 3.2) return true;
    return false;
  };
}

// candidate: what the engine would hand readTags for one row.
function candidate(id, shift, W, H, inkAt) {
  const s = TAG_STEP, cols = Math.round(W / s), rows = Math.round(H / s), ink = new Float32Array(cols * rows);
  for (let j = 0; j < rows; j++) for (let i = 0; i < cols; i++) {
    let n = 0;
    for (const a of [0.25, 0.75]) for (const b of [0.25, 0.75]) if (inkAt((i + a) * s, (j + b) * s)) n++;
    ink[j * cols + i] = n / 4;
  }
  return { id, shift, cols, rows, ink };
}

// lineUpScene: tags W × H, `across` of them gap apart, rows gapV apart; row j
// printed with rows[j]'s shift plus the printer's error. slip: how much wider
// the roll's gap is than the template's, so tag i's print sits i × slip left.
function lineUpScene({ W, H, across, gap, gapV, rows, err, slip = 0 }) {
  const rollW = across * W + (across - 1) * gap;
  return (x, y) => {
    if (x < -2.5 || x > rollW + 2.5) return 95 + 20 * Math.sin(x * 0.7) * Math.cos(y * 0.5);
    const j = Math.floor(y / (H + gapV)), ly = y - j * (H + gapV), i = Math.floor(x / (W + gap)), lx = x - i * (W + gap);
    if (j < 0 || j >= rows.length || ly > H || i < 0 || i >= across || lx > W) return 196;
    const r = rows[j];
    return r.ink(lx - r.shift - err.x + i * slip, ly - err.y) ? 38 : 238;
  };
}

const LINE_UP = [-1, -0.5, 0, 0.5, 1];
function lineUpCase({ err, turn = 0, seed = 3, show = [1, 2], slip = 0 }) {
  const W = 14.3, H = 30, cur = -0.5;
  const shifts = LINE_UP.map(s => cur + s);
  const cands = shifts.map((s, k) => Object.assign(candidate('ABCDE'[k], s, W, H, lineUpInk(k, W, H)), { kind: 'line' }));
  const rows = show.map(k => ({ shift: shifts[k], ink: lineUpInk(k, W, H) }));
  const paper = lineUpScene({ W, H, across: 3, gap: 3.2, gapV: 3, rows, err, slip });
  const img = photo(paper, { x0: -5, y0: -2, x1: 55, y1: rows.length * 33 + 1 }, 620, 700, { turn, seed });
  return { img, cands, roll: { w: W, pitch: W + 3.2, perRow: 3 } };
}

test('Line up with no check frame: the printer error is read from rows B and C', () => {
  const { img, cands, roll } = lineUpCase({ err: { x: -1.7, y: 0.4 } });
  const t0 = Date.now();
  const res = readTags(img, roll, cands);
  const ms = Date.now() - t0;
  assert.ok(res.ok, brief(res));
  near(res.shift, 1.7, 0.25, 'shift needed');
  assert.equal(res.flipped, false);
  assert.deepEqual([...new Set(res.picks.map(p => p.id))].sort(), ['B', 'C'], brief(res));
  assert.ok(ms < 8000, `took ${ms} ms`);
});

test('Line up photographed upside down reads the same', () => {
  const { img, cands, roll } = lineUpCase({ err: { x: -1.7, y: 0.4 }, turn: 2, seed: 11 });
  const res = readTags(img, roll, cands);
  assert.ok(res.ok, brief(res));
  near(res.shift, 1.7, 0.25, 'shift needed');
  assert.equal(res.flipped, true);
});

test('Line up printed the other way: a shift left is read', () => {
  const { img, cands, roll } = lineUpCase({ err: { x: 1.2, y: -0.3 }, show: [2, 3], seed: 21 });
  const res = readTags(img, roll, cands);
  assert.ok(res.ok, brief(res));
  near(res.shift, -1.2, 0.25, 'shift needed');
});

test('Line up on a roll with a wider gap across: the gap is read, the shift is for the middle tag', () => {
  const { img, cands, roll } = lineUpCase({ err: { x: -1.7, y: 0.4 }, slip: 0.5, seed: 5 });
  const res = readTags(img, roll, cands);
  assert.ok(res.ok, brief(res));
  near(res.gap, 0.5, 0.15, 'gap');
  near(res.shift, 2.2, 0.25, 'shift needed');
  assert.ok(res.drift < 0.3, brief(res));
});

test('a photo of one print: tags that match another print are left out', () => {
  // Rows B and C of a Line up, and below them a label print (a block and a
  // barcode) that landed 1 mm off what its candidate says.
  const W = 14.3, H = 30, cur = -0.5, err = { x: -1.7, y: 0.4 };
  const shifts = LINE_UP.map(s => cur + s);
  const label = (x, y) => (x >= 3 && x <= W - 3 && ((y >= 4 && y <= 5.5) || (y >= 7 && y <= 8.5))) || (x >= 3 && x <= W - 3 && y >= 14 && y <= 24 && (x - 3) % 1.2 < 0.6);
  const cands = shifts.map((s, k) => Object.assign(candidate('ABCDE'[k], s, W, H, lineUpInk(k, W, H)), { kind: 'line' }));
  cands.push(candidate('label', cur, W, H, label));
  const rows = [1, 2].map(k => ({ shift: shifts[k], ink: lineUpInk(k, W, H) })).concat([{ shift: cur + 1, ink: label }]);
  const paper = lineUpScene({ W, H, across: 3, gap: 3.2, gapV: 3, rows, err });
  const img = photo(paper, { x0: -5, y0: -2, x1: 55, y1: rows.length * 33 + 1 }, 620, Math.round(620 * (rows.length * 33 + 3) / 60), { seed: 9 });
  const res = readTags(img, { w: W, pitch: W + 3.2, perRow: 3 }, cands);
  assert.ok(res.ok, brief(res));
  assert.ok(res.picks.every(p => p.kind === 'line'), brief(res));
  near(res.shift, 1.7, 0.25, 'shift needed');
  assert.ok(res.drift < 0.3, brief(res));
});

test('no tags in the photo', () => {
  const blank = { width: 300, height: 300, data: new Uint8ClampedArray(300 * 300 * 4).fill(120) };
  const res = readTags(blank, { w: 14.3, pitch: 17.5, perRow: 3 }, [candidate('A', 0, 14.3, 30, lineUpInk(0, 14.3, 30))]);
  assert.equal(res.ok, false);
  assert.equal(res.why, 'tags');
});
