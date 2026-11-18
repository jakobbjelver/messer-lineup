/**
 * The identity key — the TypeScript twin of `internal/model/key.go`.
 *
 * `key` decides whether two names are the same thing: it joins rows to a taste list. `loose` only nominates.
 * The algorithm is not free here: `internal/model/testdata/keys.json` is the shared vector file and both
 * implementations are diffed against it, so this file is changed to match the vectors and never the reverse.
 * `make keycheck` runs the diff.
 *
 * Steps, in order (identical to Go's, including the order — lowercasing before NFKD is what makes ø → o):
 *
 *   1  NFKD, drop combining marks            Björk → bjork · Sigur Rós → sigur ros
 *   2  transliterate what NFKD cannot decompose
 *                                            ø→o · æ→ae · œ→oe · ß→ss · þ→th · ð→d · ł→l · đ→d · ı→i
 *   3  lowercase
 *   4  "&" → " and "
 *   5  drop apostrophes and quotation marks  don't → dont · "Weird Al" → weird al
 *   6  cut at the first " feat." / " ft." / " featuring "
 *   7  every run of [^a-z0-9] → "-", then trim
 *   8  empty → "x-" + sha1(name)[:10]        ¥$ → x-ebd798351b
 */

const TRANSLIT: Record<string, string> = {
  ø: "o",
  æ: "ae",
  œ: "oe",
  ß: "ss",
  þ: "th",
  ð: "d",
  ł: "l",
  đ: "d",
  ı: "i",
};

// Apostrophes and quotation marks are deleted, not replaced: don't → dont, not don-t.
const QUOTES = /['’‘`´"“”„‚]/g;
const CREDIT_MARKERS = [" feat.", " ft.", " featuring "];

/** NFKD, then drop every combining mark (Mn) — the diaeresis in Björk, the accents in Rós. */
function stripMarks(input: string): string {
  return input.normalize("NFKD").replace(/\p{Mn}/gu, "");
}

function creditIndex(input: string): number {
  let at = -1;
  for (const marker of CREDIT_MARKERS) {
    const i = input.indexOf(marker);
    if (i >= 0 && (at < 0 || i < at)) at = i;
  }
  return at;
}

function normalize(input: string): string {
  let s = stripMarks(input.toLowerCase());
  s = s.replace(/[øæœßþðłđı]/g, (ch) => TRANSLIT[ch] ?? ch);
  s = s.replace(/&/g, " and ");
  s = s.replace(QUOTES, "");
  const at = creditIndex(s);
  if (at >= 0) s = s.slice(0, at);
  s = s.replace(/[^a-z0-9]+/g, "-");
  return s.replace(/^-+|-+$/g, "");
}

/** SHA-1 of a UTF-8 string, hex. Synchronous on purpose: `Key` is a pure function, not a promise. */
export function sha1Hex(input: string): string {
  const bytes = new TextEncoder().encode(input);
  const bitLength = bytes.length * 8;

  const padded = new Uint8Array(((bytes.length + 8) >> 6) * 64 + 64);
  padded.set(bytes);
  padded[bytes.length] = 0x80;
  const view = new DataView(padded.buffer);
  view.setUint32(padded.length - 4, bitLength >>> 0, false);
  view.setUint32(padded.length - 8, Math.floor(bitLength / 0x100000000), false);

  let h0 = 0x67452301;
  let h1 = 0xefcdab89;
  let h2 = 0x98badcfe;
  let h3 = 0x10325476;
  let h4 = 0xc3d2e1f0;
  const w = new Uint32Array(80);

  for (let offset = 0; offset < padded.length; offset += 64) {
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(offset + i * 4, false);
    for (let i = 16; i < 80; i++) {
      const n = w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16];
      w[i] = (n << 1) | (n >>> 31);
    }
    let a = h0;
    let b = h1;
    let c = h2;
    let d = h3;
    let e = h4;
    for (let i = 0; i < 80; i++) {
      let f: number;
      let k: number;
      if (i < 20) {
        f = (b & c) | (~b & d);
        k = 0x5a827999;
      } else if (i < 40) {
        f = b ^ c ^ d;
        k = 0x6ed9eba1;
      } else if (i < 60) {
        f = (b & c) | (b & d) | (c & d);
        k = 0x8f1bbcdc;
      } else {
        f = b ^ c ^ d;
        k = 0xca62c1d6;
      }
      const temp = (((a << 5) | (a >>> 27)) + f + e + k + w[i]) >>> 0;
      e = d;
      d = c;
      c = (b << 30) | (b >>> 2);
      b = a;
      a = temp;
    }
    h0 = (h0 + a) >>> 0;
    h1 = (h1 + b) >>> 0;
    h2 = (h2 + c) >>> 0;
    h3 = (h3 + d) >>> 0;
    h4 = (h4 + e) >>> 0;
  }

  return [h0, h1, h2, h3, h4].map((word) => word.toString(16).padStart(8, "0")).join("");
}

/** The identity key of an artist or a venue. Never empty. */
export function Key(name: string): string {
  const key = normalize(name);
  if (key) return key;
  // Nothing survived normalisation: derive a stable key from the name as given, so an unpronounceable pair of
  // symbols still has an identity instead of joining everything to everything.
  return `x-${sha1Hex(name).slice(0, 10)}`;
}
