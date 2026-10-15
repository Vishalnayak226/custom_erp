'use strict';
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { spawnSync } = require('node:child_process');
const { server } = require('./serve.cjs');
const { validate } = require('./validate.cjs');
const root = path.resolve(__dirname, '..');
const project = JSON.parse(fs.readFileSync(path.join(root, 'project.json'), 'utf8'));
const errors = validate(project);
if (errors.length) throw new Error(errors.join('\n'));
const targetId = process.argv[2];
if (!targetId) throw new Error('Usage: node assemble.cjs <chapter-id|all-ready>');
const ready = chapter => chapter.segments.every(id => project.segments.find(s => s.id === id)?.status === 'ready');
const chapters = targetId === 'all-ready' ? project.chapters.filter(ready) : project.chapters.filter(c => c.id === targetId && ready(c));
if (!chapters.length) throw new Error('No complete chapter selected. Pending segments cannot be exported as a finished SOP.');
const outputDir = path.resolve(process.env.SOP_OUTPUT_DIR || path.join(process.env.TEMP || '.', 'erp-sop-video'));
if (outputDir.startsWith(root + path.sep)) throw new Error('Render to scratch outside the project, then review/copy in.');
fs.mkdirSync(outputDir, { recursive: true });
const output = path.join(outputDir, `${targetId}.webm`);
const raw = path.join(outputDir, `${targetId}.raw.webm`);
const playwright = require(require.resolve('playwright', { paths: [process.cwd(), process.env.USERPROFILE || process.env.HOME || ''] }));

function findFfmpeg() {
  if (process.env.SOP_FFMPEG) return process.env.SOP_FFMPEG;
  const cache = process.env.PLAYWRIGHT_BROWSERS_PATH || (process.platform === 'win32'
    ? path.join(os.homedir(), 'AppData', 'Local', 'ms-playwright')
    : path.join(os.homedir(), '.cache', 'ms-playwright'));
  if (!fs.existsSync(cache)) return null;
  const releases = fs.readdirSync(cache).filter(name => /^ffmpeg-\d+$/.test(name)).sort().reverse();
  for (const release of releases) {
    const candidate = path.join(cache, release, process.platform === 'win32' ? 'ffmpeg-win64.exe' : 'ffmpeg-linux');
    if (fs.existsSync(candidate)) return candidate;
  }
  return null;
}

async function main() {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const browser = await playwright.chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  const file = fs.openSync(raw, 'w');
  let chunks = 0;
  await page.exposeBinding('writeSopVideoChunk', (_, base64) => { fs.writeSync(file, Buffer.from(base64, 'base64')); chunks++; });
  try {
    await page.goto(origin + '/index.html', { waitUntil: 'domcontentloaded' });
    const result = await page.evaluate(async ({ chapters, project, origin }) => {
      const canvas = document.createElement('canvas'); canvas.width = 1280; canvas.height = 720;
      const ctx = canvas.getContext('2d', { alpha: false });
      const recorder = new MediaRecorder(canvas.captureStream(25), { mimeType: 'video/webm;codecs=vp8', videoBitsPerSecond: 1800000 });
      let chunkChain = Promise.resolve();
      recorder.ondataavailable = event => {
        if (!event.data.size) return;
        chunkChain = chunkChain.then(() => new Promise((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => window.writeSopVideoChunk(String(reader.result).split(',')[1]).then(resolve, reject);
          reader.onerror = reject;
          reader.readAsDataURL(event.data);
        }));
      };
      const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
      const fill = (label, title, body) => {
        ctx.fillStyle = '#17243a'; ctx.fillRect(0, 0, 1280, 720);
        ctx.fillStyle = '#a8c5ee'; ctx.font = '700 22px system-ui'; ctx.textAlign = 'center'; ctx.fillText(label.toUpperCase(), 640, 265);
        ctx.fillStyle = '#ffffff'; ctx.font = '700 42px system-ui';
        const words = title.split(' '); let line = '', lines = [];
        for (const word of words) { const next = line ? line + ' ' + word : word; if (ctx.measureText(next).width > 1020 && line) { lines.push(line); line = word; } else line = next; }
        if (line) lines.push(line);
        lines.forEach((text, i) => ctx.fillText(text, 640, 325 + i * 54));
        ctx.font = '24px system-ui'; ctx.fillStyle = '#dbe6f6';
        const bodyWords = body.split(' '); line = ''; lines = [];
        for (const word of bodyWords) { const next = line ? line + ' ' + word : word; if (ctx.measureText(next).width > 1050 && line) { lines.push(line); line = word; } else line = next; }
        if (line) lines.push(line);
        lines.forEach((text, i) => ctx.fillText(text, 640, 475 + i * 35));
      };
      const card = async (label, title, body, duration) => {
        fill(label, title, body);
        const timer = setInterval(() => fill(label, title, body), 40);
        await sleep(duration);
        clearInterval(timer);
      };
      const play = async relative => {
        const video = document.createElement('video'); video.muted = true; video.playsInline = true; video.preload = 'auto'; video.src = origin + '/' + relative;
        await new Promise((resolve, reject) => { video.onloadedmetadata = resolve; video.onerror = () => reject(new Error(`Could not load ${relative}`)); });
        await video.play();
        const draw = () => { if (video.readyState >= 2) ctx.drawImage(video, 0, 0, 1280, 720); };
        draw(); const timer = setInterval(draw, 40);
        await new Promise((resolve, reject) => { video.onended = resolve; video.onerror = () => reject(new Error(`Playback failed: ${relative}`)); });
        clearInterval(timer); video.removeAttribute('src'); video.load();
      };
      recorder.start(1000);
      for (const chapter of chapters) {
        await card('Process start', chapter.title, `${chapter.actor}. ${chapter.start}`, 2400);
        for (const id of chapter.segments) {
          const segment = project.segments.find(s => s.id === id);
          await card('Next step', segment.title, segment.actor, 900);
          await play(segment.file);
        }
        await card('Process end', chapter.title, chapter.end, 2600);
      }
      const done = new Promise(resolve => recorder.onstop = resolve);
      recorder.stop(); await done; await chunkChain;
      return { chapters: chapters.map(c => c.id), segments_played: chapters.reduce((n, c) => n + c.segments.length, 0) };
    }, { chapters, project, origin });
    fs.closeSync(file);
    const ffmpeg = findFfmpeg();
    if (ffmpeg) {
      const remux = spawnSync(ffmpeg, ['-hide_banner', '-loglevel', 'error', '-i', raw, '-c', 'copy', '-y', output], { encoding: 'utf8' });
      if (remux.status !== 0) throw new Error(`Video remux failed: ${remux.stderr}`);
    } else fs.copyFileSync(raw, output);
    console.log(JSON.stringify({ ...result, output, bytes: fs.statSync(output).size, chunks, seekable: Boolean(ffmpeg) }));
  } finally {
    try { fs.fstatSync(file); fs.closeSync(file); } catch { /* already closed after capture */ }
    await browser.close(); await new Promise(resolve => server.close(resolve));
  }
}
main().catch(error => { console.error(error.stack || error); process.exitCode = 1; });
