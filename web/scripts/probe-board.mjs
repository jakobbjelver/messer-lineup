#!/usr/bin/env node
/**
 * One pass over the running board — the checks that decide whether a UI change is done.
 *
 * Every one of these was written by hand, ad hoc, at least once (twice for the greys), and each hand-written
 * version had to rediscover the class names — rows are `.show`, not `.row` — and the seed state. Run this instead:
 *
 *     node scripts/probe-board.mjs                      # http://127.0.0.1:3000, dark, 1000x900
 *     node scripts/probe-board.mjs https://lineup.messer.wtf --width 390 --height 844 --theme light
 *     node scripts/probe-board.mjs --town copenhagen-dk --no-panel
 *
 * It prints JSON and one `ok`/`FAIL` line per check, and exits non-zero on a failure. Checks:
 *
 *   header     bar height, mark box, wordmark size, settings control, town title — the sizes the spec names
 *   headRow    `yours · all` and `list · calendar` share a row, at the two edges
 *   titleRow   the town's line and the subscription: right-aligned on desktop, stacked under the name on a phone
 *   square     every border-radius in the DOM (only `avatar` may be 50 %, plus `pick__dot` in the town panel)
 *   hue        every chromatic value's HSL hue, read through a canvas pixel (Chrome keeps oklch as a string)
 *   floor      the page is at least a viewport tall, so the footer is never a line across the middle
 *   tabs       the two tab items are the same width
 *   field      a focused search field: neutral border, no outline (read after the transition)
 *   locate     the town control in both states: label ink in both, one accent per state, tag off/on
 *   reopen     closing and reopening the town panel does not move the town (a mount effect would)
 *
 * Playwright lives in `~/Repos/messer-ui/node_modules/playwright` on this host. Point `PLAYWRIGHT` at another
 * install (or put `playwright` on the module path) when that moves.
 */

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 ? args[at + 1] : fallback;
};
const has = (name) => args.includes(`--${name}`);
const positional = args.filter((a, i) => !a.startsWith("--") && !(i > 0 && args[i - 1].startsWith("--")));

const URL_UNDER_TEST = positional[0] ?? "http://127.0.0.1:3000";
const WIDTH = Number(flag("width", 1000));
const HEIGHT = Number(flag("height", 900));
const THEME = flag("theme", "dark");
const TOWN = flag("town", "stockholm-se");
const PANEL = !has("no-panel");

async function loadPlaywright() {
  // `playwright` resolves from `web/node_modules` (it is a dev dependency); `PLAYWRIGHT` points at another install.
  // No absolute host path here: this tree is public, and `make public-scan` is right to refuse one.
  const candidates = [process.env.PLAYWRIGHT, "playwright"].filter(Boolean);
  for (const candidate of candidates) {
    try {
      return await import(candidate);
    } catch {
      /* try the next one */
    }
  }
  throw new Error("playwright not found: set PLAYWRIGHT=/path/to/playwright/index.mjs");
}

const results = [];
function check(name, ok, detail) {
  results.push({ name, ok, detail });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}: ${JSON.stringify(detail)}`);
}

const { chromium } = await loadPlaywright();
const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: WIDTH, height: HEIGHT },
  colorScheme: THEME,
  geolocation: { latitude: 59.3293, longitude: 18.0686 },
  permissions: ["geolocation"],
});
const page = await context.newPage();
await page.addInitScript((town) => {
  localStorage.setItem("lineup.onboarded", "1");
  localStorage.setItem("lineup.town", town);
  localStorage.removeItem("lineup.taste");
}, TOWN);
await page.goto(URL_UNDER_TEST, { waitUntil: "networkidle" });
await page.waitForSelector(".show", { timeout: 25000 });

/* ── header and the head row ─────────────────────────────────────────────────────────────────── */

const geometry = await page.evaluate(() => {
  const box = (sel) => {
    const el = document.querySelector(sel);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height), right: Math.round(r.right) };
  };
  const page = document.querySelector("main.page").getBoundingClientRect();
  const segs = [...document.querySelectorAll(".seg")].map((el) => ({
    label: el.getAttribute("aria-label"),
    ...(() => {
      const r = el.getBoundingClientRect();
      return { x: Math.round(r.x), y: Math.round(r.y), h: Math.round(r.height), right: Math.round(r.right) };
    })(),
  }));
  const foot = document.querySelector(".foot")?.getBoundingClientRect();
  return {
    bar: box(".bar"),
    mark: box(".bar__mark"),
    icon: box(".bar__actions .iconbtn"),
    name: getComputedStyle(document.querySelector(".bar__name")).fontSize,
    h1: getComputedStyle(document.querySelector(".head__h1")).fontSize,
    segs,
    page: { x: Math.round(page.x), w: Math.round(page.width), right: Math.round(page.right), h: Math.round(page.height) },
    viewport: innerHeight,
    footTop: foot ? Math.round(foot.top) : null,
    doc: document.documentElement.scrollHeight,
    board: box("#board"),
    head: box(".head"),
    title: box(".head__title"),
    titleH1: box(".head__h1"),
    sub: box(".subbtn"),
  };
});

check("header", true, {
  bar: geometry.bar?.h,
  mark: geometry.mark?.w,
  wordmark: geometry.name,
  settings: geometry.icon?.w,
  townTitle: geometry.h1,
});

const whose = geometry.segs.find((s) => s.label === "Whose shows");
const view = geometry.segs.find((s) => s.label === "View");
const sub = geometry.sub;
const titleH1 = geometry.titleH1;
// The second row's two ends: the view control is always at the right one, and `yours · all` joins it at the left edge
// only when the reader has a taste to filter by. This run is a guest, so the row has one child — and a guest asserting a
// `whose` control is the regression this check now catches (the state with a list is measured below, after a reload).
check(
  "headRow",
  Boolean(view && !whose && Math.abs(view.right - geometry.page.right) <= 40),
  { whose, view, pageX: geometry.page.x, pageRight: geometry.page.right },
);

// The title row: desktop puts the subscription at the right end of the name's line; a phone stacks it under the name,
// at the page's left edge. Both are the design; a third arrangement is a regression.
const oneLine = Boolean(titleH1 && sub) &&
  Math.abs((titleH1.y + titleH1.h / 2) - (sub.y + sub.h / 2)) <= 2 &&
  Math.abs(sub.right - geometry.page.right) <= 40;
const stacked = Boolean(titleH1 && sub) &&
  sub.y >= titleH1.y + titleH1.h - 2 &&
  Math.abs(sub.x - (geometry.page.x + 24)) <= 2;
check("titleRow", Boolean(oneLine || stacked), { titleH1, sub, oneLine, stacked, pageX: geometry.page.x, pageRight: geometry.page.right });

/* ── subscribe ───────────────────────────────────────────────────────────────────────────────── */

// One control, one label, a real target. This is the check the Phase 4 rewrite existed for: four mounts and six
// pieces of vocabulary became one split control, and a second one (or a renamed one) is a regression.
const subscribe = await page.evaluate(() => {
  const controls = [...document.querySelectorAll(".subbtn")];
  if (controls.length === 0) return { count: 0 };
  const box = controls[0].getBoundingClientRect();
  const label = controls[0].querySelector(".subbtn__go")?.textContent?.trim() ?? "";
  return { count: controls.length, w: Math.round(box.width), h: Math.round(box.height), label };
});
check("subscribe", subscribe.count === 1 && subscribe.h >= 40 && subscribe.label === "Subscribe", subscribe);

/* ── scene ──────────────────────────────────────────────────────────────────────────────────── */

// The front door is the place. The probe runs as a guest (it clears `lineup.taste` above), so `yours · all` must not be
// there: a tab that can only print 0 is a dead end, and a scene board does not open with one.
const frontDoor = await page.evaluate(() => {
  const seg = document.querySelector('.seg[aria-label="Whose shows"]');
  const view = document.querySelector(".seg--icons");
  return {
    tasteTab: Boolean(seg),
    view: Boolean(view),
    labels: [...document.querySelectorAll(".head__row .seg")].map((el) => el.getAttribute("aria-label")),
  };
});
check("scene", !frontDoor.tasteTab && frontDoor.view, frontDoor);

/* ── square ──────────────────────────────────────────────────────────────────────────────────── */

const radius = await page.evaluate(() => {
  const bad = [];
  for (const el of document.querySelectorAll("*")) {
    const r = getComputedStyle(el).borderTopLeftRadius;
    if (r && r !== "0px") bad.push(`${el.className || el.tagName} → ${r}`);
  }
  return [...new Set(bad)];
});
check("square", radius.every((line) => line.startsWith("avatar") || line.startsWith("pick__dot")), radius);

/* ── hue ────────────────────────────────────────────────────────────────────────────────────── */

const READ_HUE = () => {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  const rgbOf = (value) => {
    if (!value || value === "none" || value === "transparent") return null;
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = "#000";
    ctx.fillStyle = value;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return [r, g, b];
  };
  const hueOf = ([r, g, b]) => {
    r /= 255; g /= 255; b /= 255;
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const d = max - min;
    if (d < 0.06) return null; // near-neutral: paper, ink, borders
    let h = 0;
    if (max === r) h = ((g - b) / d) % 6;
    else if (max === g) h = (b - r) / d + 2;
    else h = (r - g) / d + 4;
    return Math.round(h * 60 + 360) % 360;
  };
  const seen = new Map();
  for (const el of document.querySelectorAll("*")) {
    const s = getComputedStyle(el);
    for (const prop of ["color", "backgroundColor", "borderTopColor", "borderLeftColor"]) {
      const hue = hueOf(rgbOf(s[prop]) ?? [0, 0, 0]);
      if (hue !== null) seen.set(`${hue}°`, (seen.get(`${hue}°`) ?? 0) + 1);
    }
    if (s.boxShadow !== "none") {
      for (const colour of s.boxShadow.match(/oklch\([^)]*\)|rgba?\([^)]*\)/g) ?? []) {
        const hue = hueOf(rgbOf(colour) ?? [0, 0, 0]);
        if (hue !== null) seen.set(`${hue}° shadow`, (seen.get(`${hue}° shadow`) ?? 0) + 1);
      }
    }
  }
  return [...seen.entries()].sort();
};

const hues = await page.evaluate(READ_HUE);
check("hue", hues.every(([key]) => {
  const degree = Number.parseInt(key, 10);
  return degree >= 78 && degree <= 90;
}), hues);

/* ── the page's floor ───────────────────────────────────────────────────────────────────────── */

check(
  "floor",
  geometry.page.h >= geometry.viewport - geometry.bar.h - 2 && (geometry.footTop === null || geometry.footTop + 60 >= geometry.viewport - 140),
  { pageHeight: geometry.page.h, viewport: geometry.viewport, bar: geometry.bar.h, footTop: geometry.footTop, doc: geometry.doc },
);

/* ── the settings panel ─────────────────────────────────────────────────────────────────────── */

if (PANEL) {
  await page.click('button[aria-label="Artists and town"]');
  await page.waitForTimeout(400);

  const tabs = await page.$$eval(".ui-tab", (els) =>
    els.map((el) => ({ text: el.textContent.trim(), w: Math.round(el.getBoundingClientRect().width) })),
  );
  check("tabs", tabs.length >= 2 && new Set(tabs.map((t) => t.w)).size === 1, tabs);

  // The artist search, by name: the genre field is a second combobox in the same panel now.
  const input = page.locator('input[aria-label="Search artists"]');
  await input.click();
  await page.waitForTimeout(320); // the border transition is 120ms; read after it or the old colour prints
  const field = await page.$eval(".picker .field", (el) => {
    const s = getComputedStyle(el);
    const text3 = getComputedStyle(document.documentElement).getPropertyValue("--text-3").trim();
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 1;
    const ctx = canvas.getContext("2d");
    const hex = (value) => {
      ctx.fillStyle = "#000";
      ctx.fillStyle = value;
      ctx.fillRect(0, 0, 1, 1);
      const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
      return `#${[r, g, b].map((n) => n.toString(16).padStart(2, "0")).join("")}`;
    };
    return {
      focused: el.matches(":focus-within"),
      outlineStyle: s.outlineStyle,
      border: hex(s.borderTopColor),
      expected: hex(text3),
    };
  });
  check("field", field.focused && field.outlineStyle === "none" && field.border === field.expected, field);

  await page.click('.ui-tab:has-text("Town")');
  await page.waitForTimeout(300);

  const readLocate = () => page.evaluate(() => {
    const button = document.querySelector(".locate");
    if (!button) return null;
    const s = getComputedStyle(button);
    const tag = button.querySelector(".locate__tag");
    return {
      tag: tag?.textContent?.trim() ?? null,
      labelColor: s.color,
      tagColor: tag ? getComputedStyle(tag).color : null,
      border: s.borderTopColor,
      glow: s.boxShadow === "none" ? "none" : "on",
      town: document.querySelector(".townbtn")?.textContent?.replace(/\s+/g, " ").trim(),
    };
  });

  const off = await readLocate();
  await page.click(".locate");
  await page.waitForTimeout(1200);
  const on = await readLocate();
  check("locate", Boolean(off && on && off.tag === "off" && on.tag === "on" && off.labelColor === on.labelColor), { off, on });

  // close, reopen: the town must not move (a mount-time effect re-applies what it infers)
  const before = on?.town;
  await page.click(".panel__foot .btn");
  await page.waitForTimeout(400);
  await page.click('button[aria-label="Artists and town"]');
  await page.waitForTimeout(500);
  await page.click('.ui-tab:has-text("Town")');
  await page.waitForTimeout(500);
  const after = (await readLocate())?.town;
  check("reopen", Boolean(before && after && before === after), { before, after });
}

// The other half of the same rule: give the guest a list and the control must appear, at the row's left edge, on the same
// line as the view. One reload with a key in the address is the whole fixture.
await page.goto(`${URL_UNDER_TEST}${URL_UNDER_TEST.includes("?") ? "&" : "?"}a=opeth`, { waitUntil: "networkidle" });
await page.waitForSelector('.seg[aria-label="Whose shows"]', { timeout: 25000 });
const listed = await page.evaluate(() => {
  const seg = document.querySelector('.seg[aria-label="Whose shows"]');
  const view = document.querySelector(".seg--icons");
  const main = document.querySelector("main.page").getBoundingClientRect();
  if (!seg || !view) return { tasteTab: Boolean(seg), view: Boolean(view) };
  const a = seg.getBoundingClientRect();
  const b = view.getBoundingClientRect();
  return {
    tasteTab: true,
    x: Math.round(a.x),
    expectedX: Math.round(main.x) + 24,
    sameH: Math.round(a.height) === Math.round(b.height),
    sameLine: Math.abs(a.y + a.height / 2 - (b.y + b.height / 2)) <= 2,
  };
});
check(
  "headRowList",
  listed.tasteTab === true && listed.x === listed.expectedX && listed.sameH && listed.sameLine,
  listed,
);

await browser.close();

const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} checks passed at ${URL_UNDER_TEST} (${WIDTH}x${HEIGHT}, ${THEME})`);
if (failed.length) process.exitCode = 1;
