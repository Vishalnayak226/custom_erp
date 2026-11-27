// 290.4: "How it works" step-through. A ```steps block in a Knowledge Center
// article plays as a run of short steps: a diagram that changes step by step,
// then the step's title, text and one tip. Back / Next, the dots and the arrow
// keys move. view-help.js imports this only when an article has such a block,
// so no other screen pays for it. Text goes in by textContent, never as HTML.
//
// The block, one item per line (articles under docs/kb/getting-started show it):
//   diagram: PO=Purchase order, GRN=Goods receipt   the boxes, in reading order
//   links: PO>GRN                                   the arrows
//   ## Raise the purchase order                     starts a step
//   Tip: one tip                                    the step's tip
//   show: PO, PO>GRN                                what this step lights up
//   any other line                                  the step's text
const SVG_NS = 'http://www.w3.org/2000/svg';
const BOX_W = 136, BOX_H = 44, GAP_X = 44, GAP_Y = 22, PAD = 6;
// Below this width the boxes run top to bottom, so the labels keep their size
// on a phone instead of shrinking with a wide diagram.
const NARROW = 560;
let playerCount = 0;

function parseHelpSteps(source) {
  const flow = { nodes: [], links: [], steps: [] };
  const list = text => text.split(',').map(part => part.trim()).filter(Boolean);
  let step = null;
  source.split('\n').forEach(raw => {
    const line = raw.trim();
    if (!line) return;
    if (line.startsWith('## ')) {
      step = { title: line.slice(3).trim(), text: [], tip: '', show: [] };
      flow.steps.push(step);
      return;
    }
    const field = line.match(/^(diagram|links|show|tip):\s*(.*)$/i);
    const key = field ? field[1].toLowerCase() : '';
    if (!step && key === 'diagram') {
      list(field[2]).forEach(pair => {
        const at = pair.indexOf('=');
        const id = (at < 0 ? pair : pair.slice(0, at)).trim();
        if (id) flow.nodes.push({ id, label: (at < 0 ? id : pair.slice(at + 1)).trim() });
      });
    } else if (!step && key === 'links') {
      list(field[2]).forEach(link => {
        const [from, to] = link.split('>').map(part => (part || '').trim());
        if (from && to) flow.links.push({ id: from + '>' + to, from, to });
      });
    } else if (step && key === 'show') {
      step.show = list(field[2]).map(id => id.replace(/\s*>\s*/, '>'));
    } else if (step && key === 'tip') {
      step.tip = field[2].trim();
    } else if (step) {
      step.text.push(line);
    }
  });
  return flow;
}

// layoutHelpFlow puts each box in the column of its longest path from a
// start box, and stacks a column's boxes in reading order, centred.
function layoutHelpFlow(flow, narrow) {
  const ids = new Set(flow.nodes.map(node => node.id));
  const links = flow.links.filter(link => ids.has(link.from) && ids.has(link.to));
  const depth = new Map(flow.nodes.map(node => [node.id, 0]));
  for (let round = 0; round < flow.nodes.length; round++) {
    links.forEach(link => depth.set(link.to, Math.min(flow.nodes.length - 1, Math.max(depth.get(link.to), depth.get(link.from) + 1))));
  }
  const columns = new Map();
  flow.nodes.forEach(node => {
    const column = depth.get(node.id);
    if (!columns.has(column)) columns.set(column, []);
    columns.get(column).push(node);
  });
  const deepest = Math.max(0, ...columns.keys());
  const widest = Math.max(1, ...[...columns.values()].map(column => column.length));
  const at = new Map();
  columns.forEach((column, index) => column.forEach((node, row) => {
    const across = row + (widest - column.length) / 2;
    at.set(node.id, narrow
      ? { x: PAD + across * (BOX_W + GAP_X / 2), y: PAD + index * (BOX_H + GAP_Y * 1.6) }
      : { x: PAD + index * (BOX_W + GAP_X), y: PAD + across * (BOX_H + GAP_Y) });
  }));
  const width = narrow ? widest * BOX_W + (widest - 1) * GAP_X / 2 : (deepest + 1) * BOX_W + deepest * GAP_X;
  const height = narrow ? (deepest + 1) * BOX_H + deepest * GAP_Y * 1.6 : widest * BOX_H + (widest - 1) * GAP_Y;
  return { at, links, width: width + PAD * 2, height: height + PAD * 2 };
}

function svgEl(name, attrs) {
  const el = document.createElementNS(SVG_NS, name);
  Object.entries(attrs || {}).forEach(([key, value]) => el.setAttribute(key, value));
  return el;
}

function drawHelpFlow(flow, narrow, uid) {
  const { at, links, width, height } = layoutHelpFlow(flow, narrow);
  const svg = svgEl('svg', { class: 'hs-diagram', viewBox: `0 0 ${width} ${height}`, role: 'img' });
  svg.style.maxWidth = width + 'px';
  const defs = svgEl('defs');
  ['', '-on'].forEach(kind => {
    const marker = svgEl('marker', { id: uid + kind, viewBox: '0 0 10 10', refX: '9', refY: '5', markerWidth: '7', markerHeight: '7', orient: 'auto-start-reverse' });
    marker.appendChild(svgEl('path', { d: 'M0 0 L10 5 L0 10 z', class: 'hs-head' + kind }));
    defs.appendChild(marker);
  });
  svg.appendChild(defs);
  const parts = new Map();
  links.forEach(link => {
    const a = at.get(link.from), b = at.get(link.to);
    let d;
    if (narrow) {
      const x1 = a.x + BOX_W / 2, y1 = a.y + BOX_H, x2 = b.x + BOX_W / 2, y2 = b.y - 2, mid = (y1 + y2) / 2;
      d = `M${x1} ${y1} C${x1} ${mid} ${x2} ${mid} ${x2} ${y2}`;
    } else {
      const x1 = a.x + BOX_W, y1 = a.y + BOX_H / 2, x2 = b.x - 2, y2 = b.y + BOX_H / 2, mid = (x1 + x2) / 2;
      d = `M${x1} ${y1} C${mid} ${y1} ${mid} ${y2} ${x2} ${y2}`;
    }
    const path = svgEl('path', { d, class: 'hs-link', 'marker-end': `url(#${uid})` });
    svg.appendChild(path);
    parts.set(link.id, path);
  });
  flow.nodes.forEach(node => {
    const p = at.get(node.id);
    const group = svgEl('g', { class: 'hs-node' });
    group.appendChild(svgEl('rect', { x: p.x, y: p.y, width: BOX_W, height: BOX_H, rx: '8' }));
    const text = svgEl('text', { x: p.x + BOX_W / 2, y: p.y + BOX_H / 2 });
    text.textContent = node.label;
    group.appendChild(text);
    svg.appendChild(group);
    parts.set(node.id, group);
  });
  return { svg, parts };
}

function mountHelpSteps(pre) {
  const flow = parseHelpSteps(pre.textContent || '');
  if (flow.steps.length === 0 || !pre.parentNode) return;
  const uid = 'hs-arrow-' + (++playerCount);
  const player = document.createElement('div');
  player.className = 'hs-player';
  player.tabIndex = 0;
  player.setAttribute('role', 'group');
  player.setAttribute('aria-roledescription', 'step-through');
  player.setAttribute('aria-label', flow.steps[0].title);
  const stage = document.createElement('div');
  const live = document.createElement('div');
  live.setAttribute('aria-live', 'polite');
  const count = document.createElement('p');
  count.className = 'hs-count';
  const title = document.createElement('p');
  title.className = 'hs-title';
  const text = document.createElement('p');
  text.className = 'hs-text';
  const tip = document.createElement('p');
  tip.className = 'hs-tip';
  live.append(count, title, text, tip);
  const nav = document.createElement('div');
  nav.className = 'hs-nav';
  const back = document.createElement('button');
  back.type = 'button';
  back.className = 'btn btn-outline btn-sm';
  back.textContent = '← Back';
  const next = document.createElement('button');
  next.type = 'button';
  next.className = 'btn btn-primary btn-sm';
  const dots = document.createElement('div');
  dots.className = 'hs-dots';
  flow.steps.forEach((step, index) => {
    const dot = document.createElement('button');
    dot.type = 'button';
    dot.className = 'hs-dot';
    dot.setAttribute('aria-label', `Step ${index + 1}: ${step.title}`);
    dot.addEventListener('click', () => show(index));
    dots.appendChild(dot);
  });
  nav.append(back, dots, next);
  player.append(stage, live, nav);

  let current = 0, narrow = null, drawing = null;
  function draw() {
    const nowNarrow = (player.clientWidth || NARROW) < NARROW;
    if (nowNarrow === narrow && drawing) return;
    narrow = nowNarrow;
    drawing = drawHelpFlow(flow, narrow, uid);
    stage.replaceChildren(drawing.svg);
  }
  function show(index) {
    current = Math.max(0, Math.min(flow.steps.length - 1, index));
    const step = flow.steps[current];
    const seen = new Set(flow.steps.slice(0, current).flatMap(s => s.show));
    const lit = new Set(step.show);
    drawing.parts.forEach((el, id) => {
      const state = lit.has(id) ? 'on' : seen.has(id) ? 'seen' : 'off';
      el.setAttribute('class', (id.includes('>') ? 'hs-link ' : 'hs-node ') + state);
      if (id.includes('>')) el.setAttribute('marker-end', `url(#${uid}${state === 'on' ? '-on' : ''})`);
    });
    const litLabels = flow.nodes.filter(node => lit.has(node.id)).map(node => node.label);
    drawing.svg.setAttribute('aria-label', litLabels.length ? litLabels.join(', ') : step.title);
    count.textContent = `Step ${current + 1} of ${flow.steps.length}`;
    title.textContent = step.title;
    text.textContent = step.text.join(' ');
    tip.textContent = step.tip ? 'Tip: ' + step.tip : '';
    tip.hidden = !step.tip;
    back.disabled = current === 0;
    next.textContent = current === flow.steps.length - 1 ? 'Start again' : 'Next →';
    [...dots.children].forEach((dot, i) => {
      dot.classList.toggle('current', i === current);
      dot.setAttribute('aria-current', i === current ? 'step' : 'false');
    });
  }
  back.addEventListener('click', () => show(current - 1));
  next.addEventListener('click', () => show(current === flow.steps.length - 1 ? 0 : current + 1));
  player.addEventListener('keydown', event => {
    const to = { ArrowRight: current + 1, ArrowDown: current + 1, ArrowLeft: current - 1, ArrowUp: current - 1, Home: 0, End: flow.steps.length - 1 }[event.key];
    if (to === undefined || event.altKey || event.ctrlKey || event.metaKey) return;
    event.preventDefault();
    show(to);
  });

  pre.replaceWith(player);
  draw();
  show(0);
  if ('ResizeObserver' in window) {
    // Redraw on the next frame: redrawing inside the callback resizes the
    // player again and the browser reports a ResizeObserver loop.
    let queued = false;
    new ResizeObserver(() => {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => {
        queued = false;
        if (!player.isConnected) return;
        const before = narrow;
        draw();
        if (narrow !== before) show(current);
      });
    }).observe(player);
  }
}

export { mountHelpSteps, parseHelpSteps };
