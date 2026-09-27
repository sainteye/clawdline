// Screenshot helper: node scripts/shot.mjs <url> <width> <out.png> [height]
// Uses playwright-core with the locally installed Chromium headless shell.
import { chromium } from 'playwright-core';

const [url, width = '1280', out = 'shot.png', height = '800'] = process.argv.slice(2);
if (!url) { console.error('usage: node scripts/shot.mjs <url> <width> <out.png> [height]'); process.exit(2); }
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: Number(width), height: Number(height) } });
await page.goto(url);
await page.screenshot({ path: out, fullPage: true });
await browser.close();
console.log(`wrote ${out} (${width}x${height})`);
