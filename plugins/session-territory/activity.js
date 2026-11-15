/** Proposed file layer renderer. Local synthetic acceptance only; not part of the v1 ZIP.
 * No data reader, network access, transcript access or implicit event clock. */
export function renderActivity(container, snapshot, {language = 'en'} = {}) {
  const zh = language.startsWith('zh');
  const node = (tag, text) => { const n = document.createElement(tag); n.textContent = text; return n; };
  container.replaceChildren();
  const heading = node('h2', zh ? '合成檔案活動示範 · 非即時資料' : 'Synthetic file activity · not live data');
  container.append(heading);
  if (!snapshot || snapshot.synthetic !== true) {
    container.append(node('p', zh ? '檔案活動不可用。' : 'File activity unavailable.'));
    return;
  }
  const grid = document.createElement('div'); grid.className = 'file-territories';
  Object.assign(grid.style, {display:'flex', flexWrap:'wrap', gap:'8px'});
  container.append(node('p', zh ? '等面積檔案格。僅顯示收到的成功事件；不是完整檔案清單或衝突偵測。' : 'Equal-area file cells. Received successful events only; not a complete file inventory or collision detector.'), grid);
  const events = snapshot.events.filter(e => e.outcome === 'succeeded');
  for (const file of snapshot.files) {
    const card = node('article', '');card.className = 'file-cell';
    Object.assign(card.style, {flex:'1 1 180px', minWidth:'0', border:'1px solid #75857b', padding:'16px', overflowWrap:'anywhere'});
    const touches = events.filter(e => e.fileRef === file.ref);
    const usable = snapshot.freshness === 'fresh' && snapshot.connection === 'connected';
    card.dataset.freshness = usable ? 'fresh' : 'stale';
    card.style.background = usable && touches.some(e => e.operation === 'write') ? '#405544' : '#24353b';
    card.append(node('h3', file.relativePath));
    card.append(node('p', usable ? (zh ? '新鮮合成快照' : 'Fresh synthetic snapshot') : (zh ? '過時，無即時熱度' : 'Stale; no live heat')));
    for (const event of touches) card.append(node('p', `${event.operation} · ${event.provenance} · ${event.observedAt}`));
    if (!touches.length) card.append(node('p', zh ? '沒有觀察到成功事件。' : 'No successful event observed.'));
    grid.append(card);
  }
  if (!snapshot.files.length) grid.append(node('p', zh ? '此合成快照沒有檔案。' : 'No files in this synthetic snapshot.'));
}
