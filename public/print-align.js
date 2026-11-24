// 251.1: a printer's alignment from a photo of the check label.
//
// The check label (printCheckLabel in print-check.js) prints, on every label
// of the roll, a frame and a dot inside the frame's top-left corner. From a
// photo, detectAlignment finds each frame, uses the dot to tell which way up
// the label is, maps label millimetres to photo pixels through the frame's
// four corners (so a photo taken at an angle still measures true), then
// walks out from the frame to the label's die-cut edges. How far the label's
// middle sits from the print's middle is the shift the printer needs.
//
// Everything is in the paper's frame, the one a printer's shift is in: x
// across the roll (right), y along the feed (down), millimetres from the
// label's top-left as the template places it. Pure functions over an
// ImageData-like { width, height, data }, so they also run under node:test.

const STROKE_MM = 0.4;      // frame line, 3 dots at 203 dpi
const SEARCH_MM = 4;        // how far past an expected edge to look for it
const STEP_MM = 0.05;
const LINES = 32, BAND = 4; // sample lines across the frame, grouped in bands

// contentBox: where a template prints, in the cell's frame (a vertical
// template's design is turned a quarter clockwise into its cell). The check
// frame goes there, so on a tail tag it lands on the head, not the tail.
export function contentBox(elements, vertical, designH) {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const e of elements || []) {
    const w = Number(e.w_mm) || 0, h = Number(e.h_mm) || 0, x = Number(e.x_mm) || 0, y = Number(e.y_mm) || 0;
    if (w <= 0 || h <= 0) continue;
    const c = vertical ? { x0: designH - y - h, y0: x, x1: designH - y, y1: x + w } : { x0: x, y0: y, x1: x + w, y1: y + h };
    x0 = Math.min(x0, c.x0); y0 = Math.min(y0, c.y0); x1 = Math.max(x1, c.x1); y1 = Math.max(y1, c.y1);
  }
  return x1 > x0 && y1 > y0 ? { x0, y0, x1, y1 } : null;
}

// checkGeometry: the marks of the check label in one label's cell (w, h as
// the label sits on the roll - cellSize, not the design's w and h). The
// frame is the middle of where the template prints, never under 30% of the
// label nor nearer than 10% to its edge; the dot sits inside its top-left.
export function checkGeometry(w, h, box) {
  const b = box && box.x1 > box.x0 && box.y1 > box.y0 ? box : { x0: 0, y0: 0, x1: w, y1: h };
  const span = (lo, hi, size) => {
    const l = Math.min(size * 0.8, Math.max((hi - lo) * 0.4, size * 0.3));
    const a = Math.max(size * 0.1, Math.min(size * 0.9 - l, (lo + hi) / 2 - l / 2));
    return [a, a + l];
  };
  const [x0, x1] = span(b.x0, b.x1, w), [y0, y1] = span(b.y0, b.y1, h);
  const short = Math.min(x1 - x0, y1 - y0);
  const s = Math.max(0.8, short * 0.22), gap = Math.max(0.6, short * 0.15);
  return { w, h, rect: { x0, y0, x1, y1 }, stroke: STROKE_MM, dot: { x: x0 + STROKE_MM + gap, y: y0 + STROKE_MM + gap, s } };
}

// checkElements: the check label as template elements in the design's
// frame, for the sticker engine to print like any label.
export function checkElements(geo, vertical, designH) {
  const at = vertical
    ? (x, y, w, h) => ({ x_mm: y, y_mm: designH - x - w, w_mm: h, h_mm: w })
    : (x, y, w, h) => ({ x_mm: x, y_mm: y, w_mm: w, h_mm: h });
  const r = geo.rect, d = geo.dot;
  return [
    Object.assign({ kind: 'box', thickness_mm: geo.stroke }, at(r.x0, r.y0, r.x1 - r.x0, r.y1 - r.y0)),
    Object.assign({ kind: 'box', thickness_mm: geo.stroke, fill: true }, at(d.x, d.y, d.s, d.s))
  ];
}

// ---------------------------------------------------------------- image ---

function grayOf(img) {
  const { width: w, height: h, data } = img;
  const g = new Float32Array(w * h);
  for (let i = 0, p = 0; i < g.length; i++, p += 4) g[i] = 0.299 * data[p] + 0.587 * data[p + 1] + 0.114 * data[p + 2];
  return g;
}

// darkMask: printed ink against the paper around it - a pixel well below
// the mean of its neighbourhood, so shade across the photo does not matter.
function darkMask(g, w, h) {
  const W1 = w + 1, sum = new Float64Array(W1 * (h + 1));
  for (let y = 0; y < h; y++) {
    let row = 0;
    for (let x = 0; x < w; x++) { row += g[y * w + x]; sum[(y + 1) * W1 + x + 1] = sum[y * W1 + x + 1] + row; }
  }
  const r = Math.max(6, Math.round(Math.min(w, h) / 30));
  const m = new Uint8Array(w * h);
  for (let y = 0; y < h; y++) {
    const ya = Math.max(0, y - r), yb = Math.min(h, y + r + 1);
    for (let x = 0; x < w; x++) {
      const xa = Math.max(0, x - r), xb = Math.min(w, x + r + 1);
      const mean = (sum[yb * W1 + xb] - sum[ya * W1 + xb] - sum[yb * W1 + xa] + sum[ya * W1 + xa]) / ((yb - ya) * (xb - xa));
      if (g[y * w + x] < mean - Math.max(18, mean * 0.2)) m[y * w + x] = 1;
    }
  }
  return m;
}

// components: 8-connected blobs of the mask, each with its pixels.
function components(m, w, h) {
  const lab = new Int32Array(w * h).fill(-1), parent = [];
  const find = a => { while (parent[a] !== a) { parent[a] = parent[parent[a]]; a = parent[a]; } return a; };
  const unite = (a, b) => { a = find(a); b = find(b); if (a !== b) parent[Math.max(a, b)] = Math.min(a, b); };
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x;
      if (!m[i]) continue;
      let l = -1;
      const look = j => { if (lab[j] >= 0) { if (l < 0) l = lab[j]; else unite(l, lab[j]); } };
      if (x > 0) look(i - 1);
      if (y > 0) { if (x > 0) look(i - w - 1); look(i - w); if (x < w - 1) look(i - w + 1); }
      if (l < 0) { l = parent.length; parent.push(l); }
      lab[i] = l;
    }
  }
  const comps = new Map();
  for (let i = 0; i < lab.length; i++) {
    if (lab[i] < 0) continue;
    const r = find(lab[i]);
    let c = comps.get(r);
    if (!c) { c = { n: 0, sx: 0, sy: 0, x0: w, y0: h, x1: 0, y1: 0, px: [] }; comps.set(r, c); }
    const x = i % w, y = (i - x) / w;
    c.n++; c.sx += x; c.sy += y;
    if (x < c.x0) c.x0 = x;
    if (x > c.x1) c.x1 = x;
    if (y < c.y0) c.y0 = y;
    if (y > c.y1) c.y1 = y;
    if (c.px.length < 400000) c.px.push(x, y);
  }
  return [...comps.values()].map(c => Object.assign(c, { cx: c.sx / c.n, cy: c.sy / c.n }));
}

// ----------------------------------------------------------------- shape ---

function hull(pts) {
  pts.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  const cross = (o, a, b) => (a[0] - o[0]) * (b[1] - o[1]) - (a[1] - o[1]) * (b[0] - o[0]);
  const lo = [], up = [];
  for (const p of pts) { while (lo.length >= 2 && cross(lo[lo.length - 2], lo[lo.length - 1], p) <= 0) lo.pop(); lo.push(p); }
  for (let i = pts.length - 1; i >= 0; i--) { const p = pts[i]; while (up.length >= 2 && cross(up[up.length - 2], up[up.length - 1], p) <= 0) up.pop(); up.push(p); }
  return lo.slice(0, -1).concat(up.slice(0, -1));
}

// quadOf: the four corners of a frame-shaped blob - the farthest pair is one
// diagonal, the points farthest either side of it the other.
function quadOf(c) {
  const rows = new Map();
  for (let k = 0; k < c.px.length; k += 2) {
    const x = c.px[k], y = c.px[k + 1], r = rows.get(y);
    if (!r) rows.set(y, [x, x]); else { if (x < r[0]) r[0] = x; if (x > r[1]) r[1] = x; }
  }
  const pts = [];
  for (const [y, [a, b]] of rows) { pts.push([a, y]); if (b !== a) pts.push([b, y]); }
  const H = hull(pts);
  if (H.length < 4) return null;
  let best = -1, ia = 0, ic = 0;
  for (let i = 0; i < H.length; i++) for (let j = i + 1; j < H.length; j++) {
    const d = (H[i][0] - H[j][0]) ** 2 + (H[i][1] - H[j][1]) ** 2;
    if (d > best) { best = d; ia = i; ic = j; }
  }
  const A = H[ia], C = H[ic];
  const side = p => (C[0] - A[0]) * (p[1] - A[1]) - (C[1] - A[1]) * (p[0] - A[0]);
  let B = null, D = null, sb = 0, sd = 0;
  for (const p of H) { const s = side(p); if (s > sb) { sb = s; B = p; } if (s < sd) { sd = s; D = p; } }
  return B && D ? [A, B, C, D] : null;
}

const segDist = (p, a, b) => {
  const dx = b[0] - a[0], dy = b[1] - a[1], L = dx * dx + dy * dy || 1;
  const t = Math.max(0, Math.min(1, ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / L));
  return Math.hypot(p[0] - a[0] - t * dx, p[1] - a[1] - t * dy);
};
const len = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1]);

// frameOf: a blob that is a frame's outline - nearly all its pixels on the
// quad's sides, the inside empty - refined to the line through the middle
// of each side, so blur and ink spread cancel out.
function frameOf(c) {
  const q = quadOf(c);
  if (!q) return null;
  const per = len(q[0], q[1]) + len(q[1], q[2]) + len(q[2], q[3]) + len(q[3], q[0]);
  let area = 0;
  for (let i = 0; i < 4; i++) area += q[i][0] * q[(i + 1) % 4][1] - q[(i + 1) % 4][0] * q[i][1];
  area = Math.abs(area) / 2;
  if (per < 60 || c.n > area * 0.6) return null;
  const t = c.n / per;                                   // line width in pixels
  if (t > Math.min(len(q[0], q[1]), len(q[1], q[2])) / 4) return null;
  const band = 2.5 * t + 1.5;
  let near = 0, seen = 0;
  for (let k = 0; k < c.px.length; k += 6) {
    const p = [c.px[k], c.px[k + 1]];
    seen++;
    if (Math.min(segDist(p, q[0], q[1]), segDist(p, q[1], q[2]), segDist(p, q[2], q[3]), segDist(p, q[3], q[0])) <= band) near++;
  }
  if (near < seen * 0.9) return null;
  const lines = [];
  for (let i = 0; i < 4; i++) {
    const a = q[i], b = q[(i + 1) % 4], L = len(a, b), ux = (b[0] - a[0]) / L, uy = (b[1] - a[1]) / L;
    const on = [];
    for (let k = 0; k < c.px.length; k += 2) {
      const dx = c.px[k] - a[0], dy = c.px[k + 1] - a[1], along = dx * ux + dy * uy;
      if (along >= L * 0.15 && along <= L * 0.85 && Math.abs(-dx * uy + dy * ux) <= band) on.push(c.px[k], c.px[k + 1]);
    }
    if (on.length < 16) return null;
    let mx = 0, my = 0, sxx = 0, syy = 0, sxy = 0;
    for (let k = 0; k < on.length; k += 2) { mx += on[k]; my += on[k + 1]; }
    mx /= on.length / 2; my /= on.length / 2;
    for (let k = 0; k < on.length; k += 2) { const ex = on[k] - mx, ey = on[k + 1] - my; sxx += ex * ex; syy += ey * ey; sxy += ex * ey; }
    const ang = 0.5 * Math.atan2(2 * sxy, sxx - syy);
    lines.push({ p: [mx, my], d: [Math.cos(ang), Math.sin(ang)] });
  }
  const meet = (l1, l2) => {
    const det = l1.d[0] * l2.d[1] - l1.d[1] * l2.d[0];
    if (Math.abs(det) < 1e-6) return null;
    const s = ((l2.p[0] - l1.p[0]) * l2.d[1] - (l2.p[1] - l1.p[1]) * l2.d[0]) / det;
    return [l1.p[0] + s * l1.d[0], l1.p[1] + s * l1.d[1]];
  };
  const corners = [];
  for (let i = 0; i < 4; i++) {
    const p = meet(lines[(i + 3) % 4], lines[i]);
    if (!p || len(p, q[i]) > band * 3) return null;
    corners.push(p);
  }
  return { corners, t };
}

// --------------------------------------------------------- perspective ---

// homography: the projective map taking four (x, y) points to four (u, v).
export function homography(src, dst) {
  const A = [], b = [];
  for (let i = 0; i < 4; i++) {
    const [x, y] = src[i], [u, v] = dst[i];
    A.push([x, y, 1, 0, 0, 0, -x * u, -y * u]); b.push(u);
    A.push([0, 0, 0, x, y, 1, -x * v, -y * v]); b.push(v);
  }
  for (let c = 0; c < 8; c++) {
    let p = c;
    for (let r = c + 1; r < 8; r++) if (Math.abs(A[r][c]) > Math.abs(A[p][c])) p = r;
    if (Math.abs(A[p][c]) < 1e-12) return null;
    [A[c], A[p]] = [A[p], A[c]]; [b[c], b[p]] = [b[p], b[c]];
    for (let r = 0; r < 8; r++) {
      if (r === c) continue;
      const f = A[r][c] / A[c][c];
      if (!f) continue;
      for (let k = c; k < 8; k++) A[r][k] -= f * A[c][k];
      b[r] -= f * b[c];
    }
  }
  const h = b.map((v, i) => v / A[i][i]);
  return (x, y) => { const z = h[6] * x + h[7] * y + 1; return [(h[0] * x + h[1] * y + h[2]) / z, (h[3] * x + h[4] * y + h[5]) / z]; };
}

function sampler(g, w, h) {
  return (x, y) => {
    if (!(x >= 0 && y >= 0 && x < w - 1 && y < h - 1)) return NaN;
    const xi = Math.floor(x), yi = Math.floor(y), fx = x - xi, fy = y - yi, i = yi * w + xi;
    return (g[i] * (1 - fx) + g[i + 1] * fx) * (1 - fy) + (g[i + w] * (1 - fx) + g[i + w + 1] * fx) * fy;
  };
}

// edgeAlong: where the label face ends, walking from `from` to `to` (mm)
// along lines at the given positions across. at(along, across) reads the
// photo. The lines are averaged, then the first drop that stands well clear
// of the photo's noise is the edge - white label to liner, or the dark
// die-cut line on a white liner (its leading side) - even when a deeper drop
// (liner to table) lies beyond it.
function edgeAlong(at, from, to, across) {
  const dir = Math.sign(to - from), n = Math.floor(Math.abs(to - from) / STEP_MM) + 1;
  const sum = new Float64Array(n), cnt = new Uint16Array(n);
  for (const c of across) {
    for (let k = 0; k < n; k++) {
      const v = at(from + dir * k * STEP_MM, c);
      if (!Number.isNaN(v)) { sum[k] += v; cnt[k]++; }
    }
  }
  const P = Array.from(sum, (s, k) => (cnt[k] && cnt[k] >= across.length / 2 ? s / cnt[k] : NaN));
  const r = 3, D = new Array(n).fill(NaN);
  for (let k = r; k < n - r; k++) D[k] = P[k + r] - P[k - r];
  const face = P.slice(0, 8).filter(v => !Number.isNaN(v)).sort((a, b) => b - a)[2];
  const real = D.filter(v => !Number.isNaN(v));
  if (!(face > 0) || real.length < 10) return null;
  const mad = median(real.map(Math.abs));
  const thr = Math.max(8, face * 0.05, 6 * 1.4826 * mad);
  const span = Math.round(0.5 / STEP_MM);
  for (let k = r + 1; k < n - r - 1; k++) {
    if (!(D[k] <= -thr)) continue;
    // The deepest point of this drop, within half a millimetre.
    let b = k;
    for (let j = k; j <= Math.min(n - r - 2, k + span) && !(D[j] > -thr / 2); j++) if (D[j] < D[b]) b = j;
    const den = D[b - 1] - 2 * D[b] + D[b + 1];
    const off = den ? 0.5 * (D[b - 1] - D[b + 1]) / den : 0;
    return from + dir * (b + Math.max(-1, Math.min(1, off))) * STEP_MM;
  }
  return null;
}

const spread = (a, b, n) => Array.from({ length: n }, (_, i) => a + (b - a) * (i + 0.5) / n);
const median = a => { const s = Float64Array.from(a).sort(), m = s.length >> 1; return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2; };
const r2 = v => Math.round(v * 100) / 100;
const fitLine = pts => { // least squares v = a + b·u over [u, v] pairs
  const n = pts.length, mu = pts.reduce((s, p) => s + p[0], 0) / n, mv = pts.reduce((s, p) => s + p[1], 0) / n;
  let suu = 0, suv = 0;
  for (const [u, v] of pts) { suu += (u - mu) ** 2; suv += (u - mu) * (v - mv); }
  return suu ? { a: mv - (suv / suu) * mu, b: suv / suu } : null;
};

// ------------------------------------------------------------- measure ---

function findFrames(comps, geo) {
  const { rect, stroke, dot } = geo;
  const half = stroke / 2;
  const mmCorners = [[rect.x0 + half, rect.y0 + half], [rect.x1 - half, rect.y0 + half], [rect.x1 - half, rect.y1 - half], [rect.x0 + half, rect.y1 - half]];
  const rw = rect.x1 - rect.x0 - stroke, rh = rect.y1 - rect.y0 - stroke;
  const dotMM = [dot.x + dot.s / 2, dot.y + dot.s / 2];
  const frames = [];
  for (const c of comps) {
    if (c.n < 40 || Math.max(c.x1 - c.x0, c.y1 - c.y0) < 24) continue;
    const f = frameOf(c);
    if (!f) continue;
    // Clockwise on screen, then start at the corner the dot sits in.
    let k = f.corners, s2 = 0;
    for (let i = 0; i < 4; i++) s2 += k[i][0] * k[(i + 1) % 4][1] - k[(i + 1) % 4][0] * k[i][1];
    if (s2 < 0) k = [k[0], k[3], k[2], k[1]];
    const xs = k.map(p => p[0]), ys = k.map(p => p[1]);
    const bx0 = Math.min(...xs), bx1 = Math.max(...xs), by0 = Math.min(...ys), by1 = Math.max(...ys);
    const inside = comps.filter(d => d !== c && d.n >= 6 && d.n < c.n && d.n > 0.25 * (d.x1 - d.x0 + 1) * (d.y1 - d.y0 + 1) &&
      d.cx > bx0 && d.cx < bx1 && d.cy > by0 && d.cy < by1);
    let best = null;
    for (let turn = 0; turn < 4; turn++) {
      const order = [0, 1, 2, 3].map(i => k[(i + turn) % 4]);
      const sideA = len(order[0], order[1]) + len(order[2], order[3]), sideB = len(order[1], order[2]) + len(order[3], order[0]);
      const ratio = (sideA / sideB) / (rw / rh);
      if (ratio < 0.6 || ratio > 1.65) continue;
      const H = homography(mmCorners, order);
      if (!H) continue;
      const want = H(dotMM[0], dotMM[1]);
      const tol = Math.min(len(order[0], order[1]), len(order[1], order[2])) * 0.12 + 2;
      for (const d of inside) {
        const e = Math.hypot(d.cx - want[0], d.cy - want[1]);
        if (e <= tol && (!best || e < best.e)) best = { e, order, H };
      }
    }
    if (best) frames.push({ corners: best.order, H: best.H });
  }
  return frames;
}

// measureLabel: one label's edges around its frame, in the print's frame.
// The sides are read in bands along the frame; a band where the label is
// not its full width (a tail tag's tail, a notch) is left out.
function measureLabel(fr, geo, at) {
  const { rect, w: W, h: Hh } = geo;
  const mm = (x, y) => { const p = fr.H(x, y); return at(p[0], p[1]); };
  const across = (x, y) => mm(x, y), along = (y, x) => mm(x, y);
  const fy = rect.y1 - rect.y0;
  const ys = spread(rect.y0 + 0.06 * fy, rect.y1 - 0.06 * fy, LINES);
  const bands = [];
  for (let i = 0; i < ys.length; i += BAND) {
    const set = ys.slice(i, i + BAND);
    const L = edgeAlong(across, rect.x0 - 0.3, -SEARCH_MM, set);
    const R = edgeAlong(across, rect.x1 + 0.3, W + SEARCH_MM, set);
    if (L !== null && R !== null) bands.push({ y: median(set), L, R });
  }
  const tolW = Math.max(1.2, W * 0.08);
  const good = bands.filter(b => Math.abs(b.R - b.L - W) <= tolW);
  const out = { frame: fr.corners.map(p => [r2(p[0]), r2(p[1])]), map: fr.H };
  if (good.length < Math.max(3, bands.length / 3)) {
    return Object.assign(out, { found: false, width: bands.length ? r2(median(bands.map(b => b.R - b.L))) : null });
  }
  const mid = median(good.map(b => (b.L + b.R) / 2));
  Object.assign(out, { found: true, width: r2(median(good.map(b => b.R - b.L))), right: r2(mid - W / 2) });
  const line = good.length >= 4 && good[good.length - 1].y - good[0].y >= 3 ? fitLine(good.map(b => [b.y, (b.L + b.R) / 2])) : null;
  if (line) out.skew = r2(Math.atan(line.b) * 180 / Math.PI);

  // The ends, each read in pairs of lines across the label's width. An end
  // counts only when it is straight across - a tail tag's tail end is not.
  const inset = Math.max(2, W * 0.15);
  const xs = spread(mid - W / 2 + inset, mid + W / 2 - inset, 12);
  const end = (from, to) => {
    const hits = [];
    for (let i = 0; i < xs.length; i += 2) {
      const e = edgeAlong(along, from, to, xs.slice(i, i + 2));
      if (e !== null) hits.push(e);
    }
    if (hits.length < 5) return null;
    const m = median(hits);
    return hits.every(v => Math.abs(v - m) <= 0.4) ? m : null;
  };
  let top = end(rect.y0 - 0.3, -SEARCH_MM), bottom = end(rect.y1 + 0.3, Hh + SEARCH_MM);
  if (top !== null && bottom !== null && Math.abs(bottom - top - Hh) > Math.max(1, Hh * 0.03)) { top = null; bottom = null; }
  if (top !== null && bottom !== null) out.down = r2((top + bottom) / 2 - Hh / 2);
  else if (top !== null) out.down = r2(top);
  else if (bottom !== null) out.down = r2(bottom - Hh);
  out.outline = [[mid - W / 2, top ?? 0], [mid + W / 2, top ?? 0], [mid + W / 2, bottom ?? Hh], [mid - W / 2, bottom ?? Hh]].map(([x, y]) => fr.H(x, y));
  return out;
}

// detectAlignment: what to add to a printer's shift right / shift down (mm)
// and skew (degrees), from a photo of the check label printed with geo.
// geo.pitch (label width + gap across, from the roll) lets several labels
// across tell a wrong gap from a shift: gap is then what to add to the
// roll's gap, and gapRight the shift to use with it.
// { ok: true, right, down, skew, gap, gapRight, drift, labels }
// or { ok: false, why: 'frame' | 'edges' | 'size', width?, labels }.
export function detectAlignment(img, geo) {
  const { width: w, height: h } = img;
  const g = grayOf(img);
  const comps = components(darkMask(g, w, h), w, h);
  const at = sampler(g, w, h);
  const frames = findFrames(comps, geo);
  if (!frames.length) return { ok: false, why: 'frame', labels: [] };
  const labels = frames.map(fr => measureLabel(fr, geo, at));
  const found = labels.filter(l => l.found);
  if (!found.length) {
    const widths = labels.map(l => l.width).filter(v => v !== null);
    return widths.length && Math.abs(median(widths) - geo.w) > Math.max(1.2, geo.w * 0.08)
      ? { ok: false, why: 'size', width: r2(median(widths)), labels }
      : { ok: false, why: 'edges', labels };
  }
  const rights = found.map(l => l.right), downs = found.filter(l => l.down !== undefined).map(l => l.down);
  const skews = found.filter(l => l.skew !== undefined).map(l => l.skew);
  const res = {
    ok: true,
    right: r2(median(rights)),
    down: downs.length ? r2(median(downs)) : null,
    skew: skews.length ? r2(median(skews)) : null,
    gap: null,
    gapRight: null,
    drift: r2(Math.max(...rights) - Math.min(...rights)),
    labels
  };
  // Labels across: number them by where each sits along the roll's width;
  // a shift that grows label by label is the gap being off.
  if (geo.pitch > 0 && found.length >= 2) {
    const ref = found[0], cy = (geo.rect.y0 + geo.rect.y1) / 2;
    const p0 = ref.map(geo.w / 2, cy), p1 = ref.map(geo.w / 2 + 1, cy);
    const ux = p1[0] - p0[0], uy = p1[1] - p0[1], pp = ux * ux + uy * uy;
    const pos = found.map(l => { const p = l.map(geo.w / 2, cy); return ((p[0] - p0[0]) * ux + (p[1] - p0[1]) * uy) / pp / geo.pitch; });
    const lo = Math.min(...pos.map(Math.round));
    const ks = pos.map(v => Math.round(v) - lo);
    if (pos.every(v => Math.abs(v - Math.round(v)) < 0.3) && new Set(ks).size >= 2) {
      const fit = fitLine(found.map((l, i) => [ks[i], l.right]));
      if (fit && Math.abs(fit.b) >= 0.3) { res.gap = r2(fit.b); res.gapRight = r2(fit.a); }
    }
  }
  return res;
}

// adviceText: the result in a few words for the screen.
export function adviceText(res) {
  const part = (v, pos, neg) => (Math.abs(v) < 0.25 ? '' : `${Math.abs(v).toFixed(1)} mm ${v > 0 ? pos : neg}`);
  const bits = [part(res.right, 'right', 'left'), res.down === null ? '' : part(res.down, 'down', 'up')].filter(Boolean);
  return bits.length ? `Move ${bits.join(', ')}` : 'Already centred';
}

// ------------------------------------------------------- no check frame ---
// 257.1: a photo with no check frame - a Line up, an alignment test or a
// label's own print. The tags are found first: the edge direction most of
// the photo shares, then, in bands along the roll, the comb of die-cut edges
// that fits the roll's labels across (its label width and gap). Each tag is
// laid flat in millimetres and matched against what could have printed on it
// (candidates the sticker engine draws), both ways up, moved across and
// along. Where it matches best is where the print landed; that less the
// shift it was printed with is the printer's own error, the same on every
// tag. A barbell tag's narrow tail does not fit the comb and is left out.

export const TAG_STEP = 0.25;   // mm grid a tag and its candidates are compared on
const CO = 4;                   // the first pass runs on 1 mm cells (CO × CO grid steps)
const MAX_DX = 6;               // a print is never further off its tag than this
const MIN_SCORE = 0.3;          // how well a tag must match a candidate to count
const EDGE_IN = 0.4;            // the die-cut edge itself is left out of the match
const MIN_INK = 0.5;            // share of a candidate's ink that must fall on the tag
const GAP_MIN = 0.3;            // mm per tag a gap across must differ by to be reported
const SC_BIN = 0.03;            // scale bins (log) the bands vote in
const SC_TILT = 8;              // bins the scale may change over the photo's height (perspective)
const SC_KEEP = 2;              // bins either side of the voted scale line a band may use

// axisAngle: the normal (degrees, -90..90) most of the photo's strong edges
// share - the roll's die-cut edges on a plain table.
function axisAngle(g, w, h) {
  const bins = new Float64Array(360);
  for (let y = 1; y < h - 1; y++) for (let x = 1; x < w - 1; x++) {
    const i = y * w + x;
    const gx = g[i - w + 1] + 2 * g[i + 1] + g[i + w + 1] - g[i - w - 1] - 2 * g[i - 1] - g[i + w - 1];
    const gy = g[i + w - 1] + 2 * g[i + w] + g[i + w + 1] - g[i - w - 1] - 2 * g[i - w] - g[i - w + 1];
    const m = Math.hypot(gx, gy);
    if (m < 40) continue;
    let a = Math.atan2(gy, gx) * 180 / Math.PI;
    if (a < 0) a += 180;
    bins[Math.floor(a * 2) % 360] += m;
  }
  let best = 0, bv = -1;
  for (let k = 0; k < 360; k++) {
    const v = bins[(k + 359) % 360] + 2 * bins[k] + bins[(k + 1) % 360];
    if (v > bv) { bv = v; best = k; }
  }
  return best / 2 > 90 ? best / 2 - 180 : best / 2;
}

// findTags: the photo turned by phi so u runs across the roll and v along
// it, cut into bands along v; in each band the tags' left (+) and right (-)
// edges that fit the comb of N tags of width W at the roll's pitch, the
// photo's scale (px/mm) left free. Thin printed lines are filtered out of
// the band first (a median across u of its bright level).
function findTags(g, w, h, roll, phi) {
  const W = roll.w, N = Math.max(1, roll.perRow), pitch = roll.pitch || W;
  const c = Math.cos(phi * Math.PI / 180), s = Math.sin(phi * Math.PI / 180);
  const U = Math.ceil(Math.abs(w * c) + Math.abs(h * s)), V = Math.ceil(Math.abs(w * s) + Math.abs(h * c));
  const at = sampler(g, w, h);
  const toXY = (u, v) => { const du = u - U / 2, dv = v - V / 2; return [w / 2 + du * c - dv * s, h / 2 + du * s + dv * c]; };
  const WIN = Math.max(16, Math.round(V / 40)), MED = Math.max(5, Math.round(U / 60)) | 1, r = MED >> 1;
  // The gap between tags may differ from the template's (that is what is
  // read), but not by much: a comb whose gap is a stroke's width is ink.
  const G = pitch - W, gTol = Math.max(1, G * 0.5);
  const rrLo = (W + Math.max(0.3, G - gTol)) / pitch, rrHi = (W + G + gTol) / pitch + 1e-9;
  const bands = [];
  // Typed buffers: this runs over every pixel, once per way the photo is read.
  const buf = new Float64Array(U * WIN), cnt = new Int32Array(U), win = new Float64Array(MED);
  for (let v0 = 0; v0 + WIN <= V; v0 += WIN) {
    cnt.fill(0);
    for (let v = v0; v < v0 + WIN; v++) {
      const dv = v - V / 2, x0 = w / 2 - (U / 2) * c - dv * s, y0 = h / 2 - (U / 2) * s + dv * c;
      for (let u = 0; u < U; u++) { const val = at(x0 + u * c, y0 + u * s); if (val === val) buf[u * WIN + cnt[u]++] = val; }
    }
    const hi = new Float64Array(U).fill(NaN);
    for (let u = 0; u < U; u++) { const n = cnt[u]; if (n < WIN * 0.8) continue; hi[u] = buf.subarray(u * WIN, u * WIN + n).sort()[Math.floor(n * 0.85)]; }
    const F = new Float64Array(U).fill(NaN);
    for (let u = r; u < U - r; u++) {
      let n = 0;
      for (let k = -r; k <= r; k++) { const x = hi[u + k]; if (x === x) win[n++] = x; }
      if (n > r) { const a = win.subarray(0, n).sort(), m = n >> 1; F[u] = n % 2 ? a[m] : (a[m - 1] + a[m]) / 2; }
    }
    const D = new Float64Array(U).fill(NaN);
    for (let u = 2; u < U - 2; u++) D[u] = (F[u + 2] - F[u - 2]) / 4;
    const real = [...D].filter(x => !Number.isNaN(x)).map(Math.abs);
    if (!real.length) continue;
    const thr = Math.max(2, median(real) * 1.4826 * 3);
    const pos = [], neg = [];
    for (let u = 3; u < U - 3; u++) {
      const d = D[u];
      if (Number.isNaN(d) || Math.abs(d) < thr) continue;
      if (d > 0 && d >= D[u - 1] && d > D[u + 1]) pos.push(u);
      if (d < 0 && d <= D[u - 1] && d < D[u + 1]) neg.push(u);
    }
    // A comb starts at one tag (L, R) and steps a pitch at a time each way;
    // each next tag snaps to the strongest edges near where it should be, as
    // real tags differ by a few pixels in width and place. The two outer
    // edges may be missing (they often merge with the backing's); the edges
    // between tags may not.
    const near = (list, u, tol) => { let b = -1, bv = 0; for (const x of list) { const e = Math.abs(D[x]); if (Math.abs(x - u) <= tol && e > bv) { bv = e; b = x; } } return b; };
    const bins = new Map();   // the best comb at each scale (3% bins)
    for (const L of pos) for (const R of neg) {
      const tw = R - L;
      if (tw < 20 || tw > U / N) continue;
      const tl = Math.max(2, Math.round(tw * 0.025)), tr = Math.max(2, Math.round(tw * 0.04));
      for (let rr = rrLo; rr <= rrHi; rr += 0.01) {
        const P = pitch * rr * tw / W;
        for (let first = 0; first < N; first++) {
          const tags = new Array(N);
          tags[first] = [L, R, true, true];
          let score = D[L] - D[R], minE = Math.min(D[L], -D[R]), miss = 0, sum = tw, full = 1;
          for (const dir of [-1, 1]) {
            let l = L;
            for (let k = first + dir; k >= 0 && k < N && miss < 9; k += dir) {
              const pl = l + dir * P;
              if (pl < 3 || pl + tw > U - 4) { miss = 9; break; }
              const nl = near(pos, pl, tl), nr = near(neg, (nl < 0 ? pl : nl) + tw, tr);
              if ((nl < 0 && k > 0) || (nr < 0 && k < N - 1) || (nl < 0 && nr < 0)) { miss = 9; break; }
              miss += (nl < 0) + (nr < 0);
              const tl2 = nl < 0 ? nr - tw : nl, tr2 = nr < 0 ? nl + tw : nr;
              if (nl >= 0) { score += D[nl]; minE = Math.min(minE, D[nl]); }
              if (nr >= 0) { score -= D[nr]; minE = Math.min(minE, -D[nr]); }
              if (nl >= 0 && nr >= 0) { sum += nr - nl; full++; }
              tags[k] = [tl2, tr2, nl >= 0, nr >= 0]; l = tl2;
            }
          }
          if (miss > 2 || score <= 0 || minE < thr) continue;
          const sc = sum / full / W, b = Math.round(Math.log(sc) / SC_BIN), had = bins.get(b);
          if (!had || score > had.score) bins.set(b, { score, tags, sc });
        }
        if (N === 1) break;
      }
    }
    if (!bins.size) continue;
    const refine = (u, sign) => {
      let b = Math.round(u), bv = sign * D[b] || -Infinity;
      for (let k = -3; k <= 3; k++) { const x = sign * D[Math.round(u) + k]; if (x > bv) { bv = x; b = Math.round(u) + k; } }
      const den = D[b - 1] - 2 * D[b] + D[b + 1];
      return b + (den ? Math.max(-1, Math.min(1, 0.5 * (D[b - 1] - D[b + 1]) / den)) : 0);
    };
    bands.push({ v0, v1: v0 + WIN, bins, refine, D });
  }
  // Printed strokes make strong combs too, but only in the bands they cross;
  // the die-cut edges run the whole tag. So the photo's scale is the one most
  // bands have a comb at. A photo taken at a slant shows near tags larger, so
  // the scale is a line along the photo, not one number.
  const vn = bd => (bd.v0 + WIN / 2) / V - 0.5;
  const votes = new Map();
  for (const bd of bands) {
    const seen = new Map();
    for (const [b, c] of bd.bins) for (let m = -SC_TILT; m <= SC_TILT; m++) {
      const a0 = Math.round(b - m * vn(bd));
      for (let d = -1; d <= 1; d++) { const key = m + ',' + (a0 + d); if (!(seen.get(key) >= c.score)) seen.set(key, c.score); }
    }
    for (const [key, sc] of seen) { const v = votes.get(key) || { n: 0, s: 0 }; v.n++; v.s += sc; votes.set(key, v); }
  }
  let top = null;
  for (const [key, v] of votes) if (!top || v.n > top.n || (v.n === top.n && v.s > top.s)) { const [m, a] = key.split(',').map(Number); top = { m, a, n: v.n, s: v.s }; }
  const windows = [];
  for (const bd of bands) {
    let best = null;
    const want = top ? top.a + top.m * vn(bd) : 0;
    for (const [b, c] of bd.bins) if (top && Math.abs(b - want) <= SC_KEEP && (!best || c.score > best.score)) best = c;
    if (!best) continue;
    // Each tag's own edges, unless its width is off the row's: then the edge
    // that faces a neighbour (an outer edge may be the backing's) and the
    // row's width.
    const ref = best.tags.map(([l, rt, hl, hr]) => [hl ? bd.refine(l, 1) : null, hr ? bd.refine(rt, -1) : null]);
    const ws = ref.filter(([l, rt]) => l !== null && rt !== null).map(([l, rt]) => rt - l);
    const T = ws.length ? median(ws) : best.sc * W;
    const tags = ref.map(([l, rt], k) => {
      if (l === null) return [rt - T, rt];
      if (rt === null || (Math.abs(rt - l - T) > T * 0.03 && N > 1)) {
        const keepR = rt !== null && (k === 0 || (k < N - 1 && Math.abs(bd.D[Math.round(rt)]) > Math.abs(bd.D[Math.round(l)])));
        return keepR ? [rt - T, rt] : [l, l + T];
      }
      return [l, rt];
    });
    windows.push({ v0: bd.v0, v1: bd.v1, score: best.score, sc: T / W, tags });
  }
  return { toXY, at, windows };
}

// runsOf: bands with the same tags make one stretch of tag (a head, or
// several labels along the feed). Ink near a tag's edge can hide it for a
// while, so a stretch carries on past bands with no comb, and past a stray
// comb, when the tags beyond sit where they were (closer the longer the
// hole).
function runsOf(windows, W) {
  const runs = [];
  for (const wn of windows) {
    let to = null;
    for (let i = runs.length - 1; i >= 0 && !to; i--) {
      const prev = runs[i][runs[i].length - 1], tol = (wn.v0 - prev.v1 <= prev.v1 - prev.v0 ? 0.25 : 0.1) * W * prev.sc;
      if (Math.abs(wn.sc / prev.sc - 1) < 0.1 && wn.tags.every((t, k) => Math.abs(t[0] - prev.tags[k][0]) < tol)) to = runs[i];
    }
    if (to) to.push(wn); else runs.push([wn]);
  }
  return runs.filter(r => r.length >= 3);
}

// flatTag: tag k of a run laid flat - ink (0 paper .. 1) on a TAG_STEP grid,
// x across from the tag's left edge, y along from the run's start; and the
// way back to photo pixels for drawing.
function flatTag(run, k, W, toXY, at) {
  const mid = run.map(wn => (wn.v0 + wn.v1) / 2);
  const Lf = fitLine(run.map((wn, i) => [mid[i], wn.tags[k][0]])), Rf = fitLine(run.map((wn, i) => [mid[i], wn.tags[k][1]]));
  const sc = median(run.map(wn => (wn.tags[k][1] - wn.tags[k][0]) / W));
  const v0 = run[0].v0, v1 = run[run.length - 1].v1;
  const nx = Math.floor(W / TAG_STEP), ny = Math.floor((v1 - v0) / sc / TAG_STEP);
  const pix = (x, y) => {
    const v = v0 + y * sc, L = Lf.a + Lf.b * v, R = Rf.a + Rf.b * v;
    return toXY(L + (x / W) * (R - L), v);
  };
  const val = new Float32Array(nx * ny);
  for (let j = 0; j < ny; j++) for (let i = 0; i < nx; i++) val[j * nx + i] = at(...pix((i + 0.5) * TAG_STEP, (j + 0.5) * TAG_STEP));
  // The paper's own level row by row: the bright end of the rows around, so
  // shade along the tag does not read as ink, nor a line across it as paper.
  const i0 = Math.ceil(EDGE_IN / TAG_STEP), i1 = nx - 1 - i0, R2 = Math.round(2 / TAG_STEP);
  const p85 = new Float32Array(ny);
  for (let j = 0; j < ny; j++) {
    const row = Array.from(val.subarray(j * nx + i0, j * nx + i1 + 1)).filter(v => !Number.isNaN(v)).sort((a, b) => a - b);
    p85[j] = row.length ? row[Math.floor(row.length * 0.85)] : NaN;
  }
  const ink = new Float32Array(nx * ny);
  for (let j = 0; j < ny; j++) {
    let face = -Infinity;
    for (let q = Math.max(0, j - R2); q <= Math.min(ny - 1, j + R2); q++) if (p85[q] > face) face = p85[q];
    for (let i = 0; i < nx; i++) {
      const v = val[j * nx + i];
      ink[j * nx + i] = face > 0 && !Number.isNaN(v) ? Math.max(0, Math.min(1, (face - v) / face)) : 0;
    }
  }
  return { nx, ny, ink, i0, i1, pix };
}

// blocks: a grid averaged over n × n cells.
function blocks(a, cols, rows, n) {
  const c = Math.floor(cols / n), r = Math.floor(rows / n), out = new Float32Array(c * r);
  for (let j = 0; j < r * n; j++) for (let i = 0; i < c * n; i++) out[Math.floor(j / n) * c + Math.floor(i / n)] += a[j * cols + i] / (n * n);
  return { a: out, cols: c, rows: r };
}

// score: correlation of a tag (columns i0..i1) with candidate cand placed
// dx, dy grid steps from the tag's origin, over the rows they share; null
// when too little of the candidate's ink falls on the tag.
// The sums of the tag and of the candidate come from running totals; only
// the cross term is summed cell by cell, and only where the two overlap.
function score(tag, cand, dx, dy) {
  const jlo = Math.max(0, dy), jhi = Math.min(tag.rows, dy + cand.rows);
  if (jhi - jlo < 2 || cand.total <= 0) return null;
  if ((cand.cum[jhi - dy] - cand.cum[jlo - dy]) / cand.total < MIN_INK) return null;
  const n = (jhi - jlo) * (tag.i1 - tag.i0 + 1);
  const so = tag.so[jhi] - tag.so[jlo], soo = tag.soo[jhi] - tag.soo[jlo];
  const ilo = Math.max(tag.i0, dx), ihi = Math.min(tag.i1, dx + cand.cols - 1);
  let se = 0, see = 0, soe = 0;
  if (ihi >= ilo) {
    const C = cand.cols + 1, q0 = ilo - dx, q1 = ihi - dx + 1, ta = tag.a, ca = cand.a;
    for (let j = jlo; j < jhi; j++) {
      const pr = (j - dy) * C, off = (j - dy) * cand.cols - dx, tr = j * tag.cols;
      se += cand.pe[pr + q1] - cand.pe[pr + q0];
      see += cand.pee[pr + q1] - cand.pee[pr + q0];
      for (let i = ilo; i <= ihi; i++) soe += ta[tr + i] * ca[off + i];
    }
  }
  const vo = soo - so * so / n, ve = see - se * se / n;
  return vo > 1e-9 && ve > 1e-9 ? (soe - so * se / n) / Math.sqrt(vo * ve) : null;
}

// prepared: a candidate with its ink per row (running) and each row's
// running sums across, of ink and of ink squared.
function prepared(a, cols, rows) {
  const cum = new Float64Array(rows + 1), C = cols + 1, pe = new Float64Array(rows * C), pee = new Float64Array(rows * C);
  for (let j = 0; j < rows; j++) {
    for (let i = 0; i < cols; i++) { const e = a[j * cols + i]; pe[j * C + i + 1] = pe[j * C + i] + e; pee[j * C + i + 1] = pee[j * C + i] + e * e; }
    cum[j + 1] = cum[j] + pe[j * C + cols];
  }
  return { a, cols, rows, cum, total: cum[rows], pe, pee };
}

// onTag: a tag's grid (columns i0..i1 count) with its running sums down the
// rows, of ink and of ink squared.
function onTag(a, cols, rows, i0, i1) {
  const so = new Float64Array(rows + 1), soo = new Float64Array(rows + 1);
  for (let j = 0; j < rows; j++) {
    let s = 0, ss = 0;
    for (let i = i0; i <= i1; i++) { const o = a[j * cols + i]; s += o; ss += o * o; }
    so[j + 1] = so[j] + s; soo[j + 1] = soo[j] + ss;
  }
  return { a, cols, rows, i0, i1, so, soo };
}

// readTags: where the print sits on the tags of a photo with no check frame.
// roll: { w, pitch, perRow } - one label's cell as the roll carries it and
// the pitch across (cell width + gap). candidates: [{ id, kind, shift, cols,
// rows, ink }] at TAG_STEP, each what a print sent with that shift right puts
// on a tag; candidates of one kind are one print (a Line up's rows), kind
// defaults to the id. { ok: true, shift, error, gap, drift, flipped, picks, labels } - shift
// is the shift right the printer needs (mm), error how far off it prints, gap
// how much wider the gap across is than the template's (null when it reads
// the same) - or { ok: false, why: 'tags' | 'print', labels }.
export function readTags(img, roll, candidates) {
  const { width: w, height: h } = img;
  const g = grayOf(img);
  const phi = axisAngle(g, w, h);
  let found = null;
  for (const a of [phi, phi > 0 ? phi - 90 : phi + 90]) {
    const f = findTags(g, w, h, roll, a);
    const runs = runsOf(f.windows, roll.w);
    const weight = runs.reduce((s, r) => s + r.length, 0);
    if (!found || weight > found.weight) found = Object.assign(f, { runs, weight });
  }
  if (!found.runs.length || !(candidates || []).length) return { ok: false, why: 'tags', labels: [] };
  const cands = [];
  for (const c of candidates) {
    for (const flip of [false, true]) {
      const a = flip ? Float32Array.from(c.ink).reverse() : c.ink;
      const b = blocks(a, c.cols, c.rows, CO);
      cands.push({ id: c.id, kind: c.kind || c.id, shift: Number(c.shift) || 0, flip, fine: prepared(a, c.cols, c.rows), coarse: prepared(b.a, b.cols, b.rows) });
    }
  }
  const minRows = Math.min(...cands.map(c => c.fine.rows));
  const picks = [], labels = [];
  for (const run of found.runs) {
    for (let k = 0; k < run[0].tags.length; k++) {
      const t = flatTag(run, k, roll.w, found.toXY, found.at);
      const fine = onTag(t.ink, t.nx, t.ny, t.i0, t.i1);
      const cb = blocks(t.ink, t.nx, t.ny, CO);
      const coarse = onTag(cb.a, cb.cols, cb.rows, Math.ceil(t.i0 / CO), Math.floor((t.i1 + 1) / CO) - 1);
      // First pass on 1 mm cells: for every place along, the best candidate,
      // way up and place across; the best of those, each most of a label
      // apart, are the labels on this stretch.
      const along = new Map();
      for (const c of cands) {
        for (let dy = -c.coarse.rows + 1; dy < coarse.rows; dy++) {
          for (let dx = -MAX_DX; dx <= MAX_DX; dx++) {
            const sc = score(coarse, c.coarse, dx, dy);
            if (sc !== null && sc >= MIN_SCORE * 0.8 && !(along.get(dy)?.sc >= sc)) along.set(dy, { sc, dx, dy, c });
          }
        }
      }
      const kept = [];
      for (const p of [...along.values()].sort((p, q) => q.sc - p.sc)) if (kept.every(q => Math.abs(q.dy - p.dy) * CO > minRows * 0.6)) kept.push(p);
      let any = false;
      for (const p of kept) {
        // Second pass on the fine grid around it, every candidate the same
        // way up (a Line up's rows differ only in their letter).
        let best = null;
        for (const c of cands) {
          if (c.flip !== p.c.flip) continue;
          for (let dy = (p.dy - 1) * CO; dy <= (p.dy + 1) * CO; dy++) {
            for (let dx = (p.dx - 1) * CO; dx <= (p.dx + 1) * CO; dx++) {
              const sc = score(fine, c.fine, dx, dy);
              if (sc !== null && (!best || sc > best.sc)) best = { sc, dx, dy, c };
            }
          }
        }
        if (!best || best.sc < MIN_SCORE) continue;
        // Sub-step across from the scores either side.
        const s0 = score(fine, best.c.fine, best.dx - 1, best.dy), s2 = score(fine, best.c.fine, best.dx + 1, best.dy);
        const den = s0 !== null && s2 !== null ? s0 - 2 * best.sc + s2 : 0;
        const dx = (best.dx + (den < 0 ? Math.max(-0.5, Math.min(0.5, 0.5 * (s0 - s2) / den)) : 0)) * TAG_STEP;
        const d = best.c.flip ? -dx : dx;
        picks.push({ id: best.c.id, kind: best.c.kind, score: r2(best.sc), flipped: best.c.flip, error: d - best.c.shift, tag: best.c.flip ? run[0].tags.length - 1 - k : k, label: labels.length });
        any = true;
        const y0 = Math.max(0, best.dy) * TAG_STEP, y1 = Math.min(t.ny, best.dy + best.c.fine.rows) * TAG_STEP;
        labels.push({ found: true, outline: [[0, y0], [roll.w, y0], [roll.w, y1], [0, y1]].map(([x, y]) => t.pix(x, y)) });
      }
      if (!any) labels.push({ found: false, outline: [[0, 0], [roll.w, 0], [roll.w, t.ny * TAG_STEP], [0, t.ny * TAG_STEP]].map(([x, y]) => t.pix(x, y)) });
    }
  }
  // The photo is of one print (a Line up's rows are one), its tags all the
  // same way up: keep the print and the way most of the score says. A tag
  // that matched anything else is outlined as not read.
  const tally = new Map(), key = p => p.kind + (p.flipped ? '/down' : '/up');
  for (const p of picks) tally.set(key(p), (tally.get(key(p)) || 0) + p.score);
  const top = [...tally].sort((a, b) => b[1] - a[1])[0];
  const use = top ? picks.filter(p => key(p) === top[0]) : [], flipped = !!use[0]?.flipped;
  for (const p of picks) if (!use.includes(p)) labels[p.label].found = false;
  if (!use.length) return { ok: false, why: 'print', labels };
  // A gap across that differs from the template's moves the print a little
  // more on each tag along: the slope over the tags. The shift is the one
  // for the middle tag, so the outer ones are off by no more than they must.
  const N = Math.max(1, roll.perRow), mid = (N - 1) / 2;
  const slope = new Set(use.map(p => p.tag)).size > 1 ? fitLine(use.map(p => [p.tag, p.error])).b : 0;
  const errs = use.map(p => p.error - slope * (p.tag - mid)), error = median(errs);
  const gap = use.length >= 4 && Math.abs(slope) >= GAP_MIN ? r2(-slope) : null;
  return { ok: true, shift: r2(-error), error: r2(error), gap, drift: r2(Math.max(...errs) - Math.min(...errs)), flipped, picks: use.map(({ label, ...p }) => p), labels };
}
