#!/usr/bin/env node
/**
 * The CSP's inline-script hash is a copy of the page.
 *
 * `script-src 'self' 'sha256-…'` refuses every inline script except the one whose bytes produce that hash, and the theme
 * bootstrap in `index.html` is inline on purpose (it runs before the first paint). So the hash in `public/_headers` must
 * equal the hash of the built page's inline script — and a build after an edit silently changes it, which is a white
 * page on a healthy origin, the worst kind of bug.
 *
 *   node web/scripts/cspcheck.mjs          # check (CI, and after `make web`)
 *   node web/scripts/cspcheck.mjs --write  # rewrite the hash in public/_headers
 */
import { createHash } from "node:crypto";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const page = readFileSync(join(root, "dist", "index.html"), "utf8");
const headersPath = join(root, "public", "_headers");

/**
 * Every absolute origin a `fetch()` in the built bundle names must be allowed by `connect-src`.
 *
 * A `connect-src` that forgets one does not break the page — it breaks one control, and it reports as Firefox's
 * "NetworkError when attempting to fetch resource", which names neither the policy nor the origin. That is how the
 * ListenBrainz import shipped broken: the board rendered perfectly while the picker's direct call was refused. The
 * bundle is the only truthful list of what the tab fetches, so the check reads it rather than the source.
 */
function assertConnectOrigins(headers) {
  const dir = join(root, "dist", "assets");
  const fetchOrigins = new Set();
  for (const file of readdirSync(dir).filter((name) => name.endsWith(".js"))) {
    const code = readFileSync(join(dir, file), "utf8");
    // `fetch("https://…")` and `fetch(`https://…`)`; a template that starts with the origin is still literal enough.
    for (const match of code.matchAll(/fetch\(\s*[`"'](https?:\/\/[^`"'/]+)/g)) fetchOrigins.add(match[1]);
  }
  const directive = headers.match(/^\s*Content-Security-Policy:.*?\bconnect-src ([^;]+);/m)?.[1]?.trim() ?? "";
  const allowed = new Set(directive.split(/\s+/).filter(Boolean));
  const missing = [...fetchOrigins].filter((origin) => !allowed.has(origin));
  if (missing.length > 0) {
    console.error(`cspcheck: the bundle fetches ${missing.join(", ")}, which connect-src does not allow.`);
    console.error(`cspcheck: connect-src is "${directive}" — add the origin to web/public/_headers.`);
    process.exit(1);
  }
  return fetchOrigins.size;
}

const headers = readFileSync(headersPath, "utf8");
const origins = assertConnectOrigins(headers);

const match = page.match(/<script>([\s\S]*?)<\/script>/);
if (!match) {
  console.error("cspcheck: dist/index.html has no inline script; the theme bootstrap moved?");
  process.exit(1);
}
const hash = `sha256-${createHash("sha256").update(match[1]).digest("base64")}`;

if (headers.includes(`'${hash}'`)) {
  console.log(`cspcheck: ${hash} matches the built page; ${origins} cross-origin fetch target(s) allowed`);
  process.exit(0);
}

if (!process.argv.includes("--write")) {
  const current = headers.match(/script-src 'self' '([^']+)'/)?.[1] ?? "(none)";
  console.error(`cspcheck: _headers carries ${current}, the built page hashes to ${hash}`);
  console.error("cspcheck: run `make csp` after the build to rewrite it");
  process.exit(1);
}

const next = headers.replace(/script-src 'self' '[^']*'/, `script-src 'self' '${hash}'`);
writeFileSync(headersPath, next);
console.log(`cspcheck: wrote ${hash} to public/_headers`);
