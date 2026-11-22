// The social card, drawn from the board's own fonts and tokens.
//
// Why a generator and not a hand-drawn SVG: the card is the only place the product's name is set as type rather than
// markup, and a standalone `.svg` cannot carry the two woff2 faces this brand uses (an embedded base64 copy would make
// the file unreadable and would drift from `public/fonts`). Chromium already renders the real faces for the design
// probe, so the card is one screenshot of a page that loads them from disk.
//
//   make og          # writes web/public/og.png
//
// Re-run it whenever the sentence, the palette or the fonts change — the copy is baked into pixels, and nothing else in
// the tree knows that.

import { chromium } from "playwright";
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const fonts = join(root, "public", "fonts");

const b64 = async (name) => (await readFile(join(fonts, name))).toString("base64");

const SENTENCE = "A season board for the live scene near you.";
const SUB = "Four towns, one board, a calendar that fills itself.";

const [sans, mono] = await Promise.all([
  b64("golos-text-latin-wght-normal.woff2"),
  b64("jetbrains-mono-latin-wght-normal.woff2"),
]);

const html = `<!doctype html>
<html><head><meta charset="utf-8"><style>
  @font-face { font-family: Golos; src: url(data:font/woff2;base64,${sans}) format("woff2"); font-weight: 400 900; font-display: block; }
  @font-face { font-family: Mono; src: url(data:font/woff2;base64,${mono}) format("woff2"); font-weight: 400 700; font-display: block; }

  /* The tokens the card shares with the board (web/src/styles/tokens.css, dark). */
  :root { --ink: #08080a; --accent: #a6e146; --text-1: #f4f4f5; --text-2: #a5a5a8; --text-3: #7d7d82; }

  * { margin: 0; box-sizing: border-box; }
  html, body { width: 1200px; height: 630px; }
  body {
    background: var(--ink);
    color: var(--text-1);
    font-family: Golos, system-ui, sans-serif;
    position: relative;
    overflow: hidden;
    padding: 72px;
  }
  /* A grid you can barely see, and one light source in the corner: the same stage-in-neon-smoke the board uses. */
  body::before {
    content: ""; position: absolute; inset: 0;
    background-image:
      repeating-linear-gradient(0deg, rgba(255,255,255,.035) 0 1px, transparent 1px 40px),
      repeating-linear-gradient(90deg, rgba(255,255,255,.035) 0 1px, transparent 1px 40px);
  }
  body::after {
    content: ""; position: absolute; right: -220px; bottom: -300px; width: 780px; height: 780px;
    background: radial-gradient(circle, rgba(166,225,70,.20) 0%, rgba(166,225,70,.06) 42%, transparent 68%);
  }
  .eyebrow { font-family: Mono, ui-monospace, monospace; font-size: 19px; font-weight: 600; letter-spacing: .34em; color: var(--accent); }
  .head { font-size: 138px; font-weight: 800; letter-spacing: -.02em; line-height: .92; margin-top: 26px; }
  .rule { width: 72px; height: 4px; background: var(--accent); margin: 30px 0 26px; }
  .tag { font-size: 31px; line-height: 1.35; color: var(--text-2); }
  .foot { position: absolute; left: 72px; right: 72px; bottom: 66px; display: flex; justify-content: space-between; align-items: baseline; font-family: Mono, ui-monospace, monospace; }
  .url { font-size: 21px; color: var(--text-1); }
  .note { font-size: 17px; color: var(--text-3); }
</style></head>
<body>
  <div class="eyebrow">MESSER</div>
  <div class="head">LINEUP</div>
  <div class="rule"></div>
  <div class="tag">${SENTENCE}<br>${SUB}</div>
  <div class="foot"><span class="url">lineup.messer.wtf</span><span class="note">No account · No session</span></div>
</body></html>`;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
await page.setContent(html, { waitUntil: "load" });
await page.evaluate(() => document.fonts.ready);
const png = await page.screenshot({ clip: { x: 0, y: 0, width: 1200, height: 630 } });
await browser.close();

const out = join(root, "public", "og.png");
await writeFile(out, png);
console.log(`wrote ${out} (${png.length} bytes, 1200×630)`);
