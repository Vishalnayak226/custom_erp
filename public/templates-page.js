// Stage 88.4 (D6, user 2026-10-18): one Templates page under Setup ▸ Printing,
// module by module - ERP documents, stickers and barcodes, OMS - each opening
// the designer it always had (the print template studio, the sticker studio).
// Setup's Print Template and Sticker Template lists and Sticker Printing's
// template tabs all lead here.
const Q = new URL(import.meta.url).search;
const OMS_TXNS = ['OMSBill', 'PickList'];

export async function renderTemplatesPage(container) {
  const [trade, studio, stickers] = await Promise.all([
    import('./trade-docs.js' + Q),
    import('./print-template-studio.js' + Q),
    import('./sticker-studio.js' + Q)
  ]);
  const sections = [['tpl-erp', 'ERP documents'], ['tpl-stickers', 'Stickers and barcodes'], ['tpl-oms', 'OMS']];
  container.insertAdjacentHTML('beforeend', `
    <div class="page-header"><div class="page-title-section"><h1 class="page-title">Templates</h1></div></div>
    <div class="tpl-rail" style="display:flex;gap:8px;flex-wrap:wrap;margin:0 0 16px;" role="navigation" aria-label="Template modules">${sections.map(([id, label]) =>
      `<button type="button" class="btn btn-outline btn-sm" data-tpl-go="${id}">${label}</button>`).join('')}</div>`);
  container.querySelectorAll('[data-tpl-go]').forEach(b => b.addEventListener('click', () =>
    document.getElementById(b.dataset.tplGo)?.scrollIntoView({ block: 'start', behavior: 'smooth' })));

  const section = (id) => {
    const el = document.createElement('section');
    el.id = id;
    el.style.cssText = 'margin-bottom:24px;scroll-margin-top:12px;';
    container.appendChild(el);
    return el;
  };
  await studio.renderPrintTemplateGallery(section('tpl-erp'), {
    only: trade.TRANSACTIONS.map(t => t.key).filter(k => !OMS_TXNS.includes(k)), title: 'ERP documents'
  });
  const stickerHost = section('tpl-stickers');
  await stickers.renderStickerTemplateGallery(stickerHost, { onChange: () => renderView(currentView) });
  await stickers.renderStickerTemplateRules(stickerHost);
  const oms = section('tpl-oms');
  await studio.renderPrintTemplateGallery(oms, { only: OMS_TXNS, title: 'OMS' });
  // The courier label is the one print with no designer: a thermal printer
  // gets ZPL with the AWB as a Code 128 barcode (engines.BuildShippingLabelPayload).
  [...oms.querySelectorAll('.pts-gallery')].pop()?.insertAdjacentHTML('afterend', `
    <div class="pts-section-title">Shipping label</div>
    <div class="pts-gallery"><div class="pts-card">
      <h3>Courier label <span class="badge badge-secondary">Built-in</span></h3>
      <div class="pts-card-meta">Ship-to pincode, courier, AWB and order. A thermal label printer gets it with the AWB as a barcode; any other printer gets the label sheet. Printed from OMS ▸ Ship or right after Pack.</div>
    </div></div>`);
}
