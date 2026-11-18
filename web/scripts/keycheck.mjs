#!/usr/bin/env node
/**
 * The identity contract, checked in both runtimes.
 *
 * `internal/model/testdata/keys.json` is canonical: Go's `model.Key` and the TypeScript twin in
 * `web/src/lib/key.ts` are both diffed against it, and a failing case means the implementation changes, never the
 * vector. Run with `make keycheck`.
 */

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { Key } from "../src/lib/key.ts";

const here = dirname(fileURLToPath(import.meta.url));
const vectors = JSON.parse(readFileSync(join(here, "../../internal/model/testdata/keys.json"), "utf8"));

let failures = 0;
for (const vector of vectors) {
  const got = Key(vector.in);
  if (got !== vector.key) {
    failures += 1;
    console.error(`MISMATCH ${JSON.stringify(vector.in)}\n  want ${vector.key}\n  got  ${got}`);
  }
}

console.log(`keycheck: ${vectors.length - failures}/${vectors.length} vectors agree with internal/model/key.go`);
process.exit(failures ? 1 : 0);
