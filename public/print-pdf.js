// Stage 98.2 (user, 2026-10-19: "I want a pdf download and print as well").
//
// A print's pages as a PDF file, made in the browser from the very pages the
// print preview draws (decision D1): each page is drawn to a canvas through an
// SVG foreignObject, kept as a JPEG, and the JPEGs are put one per page into a
// small PDF written here - no library. The PDF is a picture of the page, so it
// always looks exactly like the print; its text cannot be selected.
//
// Loaded on demand by trade-docs.js; kept out of the cold core.

const VERSION_QUERY = new URL(import.meta.url).search;
const DPI = 150;
const CSS_PX_PER_MM = 96 / 25.4;

let cssText = null;
async function tradeCSS() {
  if (cssText !== null) return cssText;
  try {
    const res = await fetch('/trade-docs.css' + VERSION_QUERY);
    cssText = res.ok ? await res.text() : '';
  } catch (_) {
    cssText = '';
  }
  return cssText;
}

// An SVG drawn as an image loads nothing from the network, so every picture
// that is not already a data: URL (a logo by address) is inlined first.
async function inlineImages(root) {
  await Promise.all([...root.querySelectorAll('img')].map(async img => {
    const src = img.getAttribute('src') || '';
    if (!src || src.startsWith('data:')) return;
    try {
      const res = await (window.apiFetch ? window.apiFetch(src) : fetch(src));
      if (!res || !res.ok) { img.remove(); return; }
      const blob = await res.blob();
      img.setAttribute('src', await new Promise((resolve, reject) => {
        const r = new FileReader();
        r.onload = () => resolve(r.result);
        r.onerror = reject;
        r.readAsDataURL(blob);
      }));
    } catch (_) {
      img.remove();
    }
  }));
}

async function pageToJPEG(page, widthMM, heightMM, css) {
  const { canvas, w, h } = await pageToCanvas(page, widthMM, heightMM, css, DPI);
  // toDataURL throws on a browser that treats the drawing as cross-origin
  // (older Safari); the caller says so and offers the print dialog instead.
  const b64 = canvas.toDataURL('image/jpeg', 0.88).split(',')[1];
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return { bytes, w, h };
}

// pageToESCPOS (Stage 106.3) is a page as ESC-POS raster commands for a
// receipt printer: drawn at the printer's own dots, cut to its printable width
// (paper less 4 mm a side, a whole number of bytes), turned black-and-white,
// sent as GS v 0 bands, then fed and cut. Returned as base64.
export async function pageToESCPOS(page, widthMM, heightMM, { paperMM = 80, dpi = 203 } = {}) {
  const css = await tradeCSS();
  if (document.fonts?.ready) await document.fonts.ready;
  const { canvas, w, h } = await pageToCanvas(page, widthMM, heightMM, css, dpi);
  const dotsPerMM = dpi / 25.4;
  const printable = Math.min(w, Math.floor(Math.max(paperMM - 8, 20) * dotsPerMM / 8) * 8);
  const left = Math.max(0, Math.floor((w - printable) / 2));
  const px = canvas.getContext('2d').getImageData(left, 0, printable, h).data;
  const rowBytes = printable / 8;
  const out = [0x1b, 0x40];
  const BAND = 128;
  for (let y0 = 0; y0 < h; y0 += BAND) {
    const rows = Math.min(BAND, h - y0);
    out.push(0x1d, 0x76, 0x30, 0, rowBytes & 0xff, rowBytes >> 8, rows & 0xff, rows >> 8);
    for (let y = y0; y < y0 + rows; y++) {
      for (let bx = 0; bx < rowBytes; bx++) {
        let byte = 0;
        for (let bit = 0; bit < 8; bit++) {
          const i = (y * printable + bx * 8 + bit) * 4;
          if (px[i] * 0.299 + px[i + 1] * 0.587 + px[i + 2] * 0.114 < 160) byte |= 0x80 >> bit;
        }
        out.push(byte);
      }
    }
  }
  out.push(0x1b, 0x64, 4, 0x1d, 0x56, 0x42, 0);
  let bin = '';
  for (let i = 0; i < out.length; i += 0x8000) bin += String.fromCharCode.apply(null, out.slice(i, i + 0x8000));
  return btoa(bin);
}

// pageToCanvas draws a rendered page onto a white canvas at `dpi`.
async function pageToCanvas(page, widthMM, heightMM, css, dpi) {
  const clone = page.cloneNode(true);
  clone.style.transform = '';
  await inlineImages(clone);
  const scale = dpi / 96;
  const w = Math.round(widthMM * CSS_PX_PER_MM * scale);
  const h = Math.round(heightMM * CSS_PX_PER_MM * scale);
  const xhtml = new XMLSerializer().serializeToString(clone);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><foreignObject x="0" y="0" width="${w}" height="${h}">` +
    `<div xmlns="http://www.w3.org/1999/xhtml" style="transform:scale(${scale});transform-origin:0 0;width:${widthMM}mm;height:${heightMM}mm;">` +
    `<style>${css.replace(/<\/style/gi, '')}</style>${xhtml}</div></foreignObject></svg>`;
  const img = new Image();
  img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
  await img.decode();
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const g = canvas.getContext('2d');
  g.fillStyle = '#fff';
  g.fillRect(0, 0, w, h);
  g.drawImage(img, 0, 0, w, h);
  return { canvas, w, h };
}

// buildPDF writes one JPEG per page at the page's size in points.
function buildPDF(images, widthMM, heightMM) {
  const enc = new TextEncoder();
  const chunks = [];
  const offsets = [];
  let length = 0;
  const push = (part) => { const b = typeof part === 'string' ? enc.encode(part) : part; chunks.push(b); length += b.length; };
  const obj = (n, body) => { offsets[n] = length; push(`${n} 0 obj\n`); body(); push('\nendobj\n'); };
  const wPt = (widthMM * 72 / 25.4).toFixed(2);
  const hPt = (heightMM * 72 / 25.4).toFixed(2);
  const n = images.length;
  push('%PDF-1.4\n%\xE2\xE3\xCF\xD3\n');
  obj(1, () => push('<< /Type /Catalog /Pages 2 0 R >>'));
  const kids = images.map((_, i) => `${3 + i * 3} 0 R`).join(' ');
  obj(2, () => push(`<< /Type /Pages /Kids [${kids}] /Count ${n} >>`));
  images.forEach((im, i) => {
    const pageN = 3 + i * 3, imgN = pageN + 1, contentN = pageN + 2;
    obj(pageN, () => push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${wPt} ${hPt}] /Resources << /XObject << /Im${i} ${imgN} 0 R >> >> /Contents ${contentN} 0 R >>`));
    obj(imgN, () => {
      push(`<< /Type /XObject /Subtype /Image /Width ${im.w} /Height ${im.h} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ${im.bytes.length} >>\nstream\n`);
      push(im.bytes);
      push('\nendstream');
    });
    const content = `q ${wPt} 0 0 ${hPt} 0 0 cm /Im${i} Do Q`;
    obj(contentN, () => push(`<< /Length ${content.length} >>\nstream\n${content}\nendstream`));
  });
  const total = 3 + n * 3;
  const xref = length;
  push(`xref\n0 ${total}\n0000000000 65535 f \n`);
  for (let i = 1; i < total; i++) push(String(offsets[i]).padStart(10, '0') + ' 00000 n \n');
  push(`trailer\n<< /Size ${total} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`);
  return new Blob(chunks, { type: 'application/pdf' });
}

// pagesToPDF turns rendered print pages (trade-docs.js renderPages) into a
// PDF Blob, page size in mm.
export async function pagesToPDF(pages, widthMM, heightMM) {
  const css = await tradeCSS();
  if (document.fonts?.ready) await document.fonts.ready;
  const images = [];
  for (const p of pages) images.push(await pageToJPEG(p, widthMM, heightMM, css));
  return buildPDF(images, widthMM, heightMM);
}

export function blobToBase64(blob) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(',')[1] || '');
    r.onerror = reject;
    r.readAsDataURL(blob);
  });
}

export function downloadBlob(blob, name) {
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  document.body.appendChild(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}
