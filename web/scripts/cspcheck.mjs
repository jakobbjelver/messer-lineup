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
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const page = readFileSync(join(root, "dist", "index.html"), "utf8");
const headersPath = join(root, "public", "_headers");

const match = page.match(/<script>([\s\S]*?)<\/script>/);
if (!match) {
  console.error("cspcheck: dist/index.html has no inline script; the theme bootstrap moved?");
  process.exit(1);
}
const hash = `sha256-${createHash("sha256").update(match[1]).digest("base64")}`;
const headers = readFileSync(headersPath, "utf8");

if (headers.includes(`'${hash}'`)) {
  console.log(`cspcheck: ${hash} matches the built page`);
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
