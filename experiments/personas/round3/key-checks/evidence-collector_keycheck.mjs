// Key check for round 3's evidence-collector fixture. Each check prints PASS when the planted
// defect (or the decoy's correct behaviour) is present. Usage, from a copy of the fixture with
// node_modules/playwright-core available:  node <this file> <fixture-copy-dir>
import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';
import path from 'node:path';

const dir = path.resolve(process.argv[2]);
const { chromium } = createRequire(path.join(dir, 'package.json'))('playwright-core');
const PORT = 4391;
const url = `http://127.0.0.1:${PORT}/`;
const server = spawn('node', ['server.mjs'], { cwd: dir, env: { ...process.env, PORT: String(PORT) }, stdio: 'ignore' });
await new Promise((r) => setTimeout(r, 500));
const browser = await chromium.launch();
const results = [];
const check = (id, ok, detail) => results.push(`${ok ? 'PASS' : 'FAIL'} ${id} ${detail}`);

async function page(width = 1280) {
  const ctx = await browser.newContext({ viewport: { width, height: 800 } });
  const p = await ctx.newPage();
  await p.goto(url);
  return p;
}
const line = (p, n) => p.locator('.line').nth(n);
const text = (p, sel) => p.locator(sel).textContent();

// E1: at 375 px the page scrolls horizontally and Checkout is cut off.
{
  const p = await page(375);
  const sw = await p.evaluate(() => document.documentElement.scrollWidth);
  const box = await p.locator('.checkout').boundingBox();
  check('E1', sw > 375 && box.x + box.width > 375, `scrollWidth=${sw} checkoutRight=${Math.round(box.x + box.width)}`);
}
// E2: at exactly 768 px the summary is stacked, not to the right.
{
  const p = await page(768);
  const items = await p.locator('.items').boundingBox();
  const sum = await p.locator('#summary').boundingBox();
  check('E2', sum.y >= items.y + items.height - 1, `items.bottom=${Math.round(items.y + items.height)} summary.top=${Math.round(sum.y)}`);
  const p2 = await page(769);
  const s2 = await p2.locator('#summary').boundingBox();
  const i2 = await p2.locator('.items').boundingBox();
  check('E2-control', s2.x > i2.x + i2.width - 1, `at 769 summary.x=${Math.round(s2.x)}`);
}
// E3: + reaches 11.
{
  const p = await page();
  for (let i = 0; i < 12; i++) if (await line(p, 0).locator('.inc').isEnabled()) await line(p, 0).locator('.inc').click();
  check('E3', (await line(p, 0).locator('.qty').textContent()) === '11', `qty=${await line(p, 0).locator('.qty').textContent()}`);
}
// E4: − reaches 0 with a $0.00 line.
{
  const p = await page();
  await line(p, 0).locator('.dec').click();
  const q = await line(p, 0).locator('.qty').textContent();
  check('E4', q === '0', `qty=${q} line=${await line(p, 0).locator('.line-total').textContent()}`);
}
// E5: discount is not recomputed after a quantity change.
{
  const p = await page();
  await p.fill('#promo', 'SAVE10');
  await p.click('#promo-form button');
  const d1 = await text(p, '#discount');
  await line(p, 1).locator('.inc').click(); // monitor 1 -> 2
  const d2 = await text(p, '#discount');
  check('E5', d1 === d2, `discount before=${d1} after=${d2} total=${await text(p, '#total')}`);
}
// E6: the error stays after a valid code.
{
  const p = await page();
  await p.fill('#promo', 'NOPE');
  await p.click('#promo-form button');
  await p.fill('#promo', 'SAVE10');
  await p.click('#promo-form button');
  check('E6', await p.locator('#promo-error').isVisible(), `error visible after valid code: "${await text(p, '#promo-error')}"`);
}
// E7: no thousands separator.
{
  const p = await page();
  const s = await text(p, '#subtotal');
  check('E7', s === '$1053.00', `subtotal=${s}`);
}
// E8: empty cart still shows summary and Checkout.
{
  const p = await page();
  while (await p.locator('.line').count()) await line(p, 0).locator('.remove').click();
  check('E8', (await p.locator('#empty').isVisible()) && (await p.locator('.checkout').isVisible()), 'empty state visible and Checkout visible');
}
// E9: quantity change is lost on reload.
{
  const p = await page();
  await line(p, 0).locator('.inc').click();
  await p.reload();
  const q = await line(p, 0).locator('.qty').textContent();
  check('E9', q === '1', `qty after + and reload=${q}`);
  // control: a removal does persist
  await line(p, 0).locator('.remove').click();
  await p.reload();
  check('E9-control', (await p.locator('.line').count()) === 2, 'removal persisted');
}
// Y1: /checkout is 404 (out of scope).
{
  const r = await fetch(url + 'checkout');
  check('Y1', r.status === 404, `status=${r.status}`);
}
// Y2: cents render as dollars.
{
  const p = await page();
  check('Y2', (await line(p, 0).locator('.line-total').textContent()) === '$129.00', 'keyboard line $129.00');
}
// Y3: lowercase code accepted.
{
  const p = await page();
  await p.fill('#promo', 'save10');
  await p.click('#promo-form button');
  check('Y3', (await p.locator('#discount-row').isVisible()) && !(await p.locator('#promo-error').isVisible()), `discount=${await text(p, '#discount')}`);
}
await browser.close();
server.kill();
console.log(results.join('\n'));
