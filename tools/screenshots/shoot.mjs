// Takes the README's screenshots of a running demo server (see run.sh): the library, a game's sheet
// and its copies, and the library on a phone. Runs in the Playwright image, so Chromium is the one
// the extension's end-to-end test uses. BASE is the server's address, OUT the folder to write to.
import { chromium } from 'playwright-core';

const base = process.env.BASE ?? 'http://host.docker.internal:8094';
const out = process.env.OUT ?? '/out';
const browser = await chromium.launch({ channel: 'chromium', headless: true });

/** A signed-in page of the given size; reduced motion, so no picture is taken mid-fade. */
async function open(width, height, phone = false) {
  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 2,
    isMobile: phone,
    hasTouch: phone,
    locale: 'en-US',
    colorScheme: 'light',
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  await page.goto(`${base}/`);
  await page.getByLabel('Username').fill('demo');
  await page.getByLabel('Password').fill('demo-screenshots-1');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForFunction(() => !document.querySelector('input[type=password]'));
  await page.evaluate(() => localStorage.setItem('gamevault.libraryGrouping', 'game'));
  return page;
}

/** Waits until the page is quiet and every picture has arrived. */
async function settle(page, extra = 800) {
  await page.waitForLoadState('networkidle');
  await page.waitForFunction(() => [...document.images].filter((i) => {
    const r = i.getBoundingClientRect();
    return r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth;
  }).every((i) => i.complete), null, { timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(extra);
}

/** Opens `hash`, runs `before` (a click…) and writes a JPEG: at twice the pixels, small enough for a README. */
async function shot(page, hash, file, extra, before) {
  await page.goto(`${base}/${hash}`);
  await settle(page, extra);
  if (before) {
    await before(page);
    await settle(page, extra);
  }
  await page.screenshot({ path: `${out}/${file}`, type: 'jpeg', quality: 82 });
  console.log(`wrote ${file}`);
}

const desk = await open(1440, 900);
// The first visit asks the providers for the covers; the second finds them stored.
await desk.goto(`${base}/#/library`);
await settle(desk, 20000);
await shot(desk, '#/library', 'library.jpg');

const id = await desk.evaluate(async () => {
  const res = await fetch('/gamevault.v1.GameService/ListGames', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ language: 'en' }),
  });
  const { games } = await res.json();
  return games.find((g) => g.title === 'Hollow Knight').id;
});
await shot(desk, `#/game/${id}`, 'game.jpg', 4000);
await shot(desk, `#/game/${id}`, 'copies.jpg', 1500, (p) => p.getByRole('tab', { name: /copies/ }).click());

const phone = await open(390, 844, true);
await shot(phone, '#/library', 'phone.jpg');

await browser.close();
