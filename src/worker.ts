/**
 * The Messer Lineup Worker.
 *
 * Assets are served first by the platform — a page view never invokes this script, which is what keeps it
 * free and unmetered. The script therefore only answers what cannot be a file: `/healthz`, `/readyz`, the
 * five `/api/v1` routes that need an account, and `/calendar/<spec>.ics`.
 *
 * The index is *not* proxied. It lives in the R2 bucket behind `lineup-data.messer.wtf` and the board fetches
 * it directly, so a shard never travels through a Worker and never costs CPU.
 *
 * The routes that need accounts answer 501 in the contract's error envelope until the taste providers land
 * (P5). That is deliberate: the envelope and the status are the contract `scripts/contract.mjs` diffs against
 * `lineup serve`, so a client written today will not need changing when the route becomes real.
 */

import { Hono } from "hono";

export interface Env {
  /** `https://lineup-data.messer.wtf` — where the built index is published. Read only by /readyz. */
  DATA_BASE: string;
  /**
   * The static assets, bound so the one client route can be resolved without moving page views onto the script.
   * `/a/<artist key>` has no file behind it; the asset layer's `single-page-application` rule answers it with
   * index.html once the script asks it to.
   */
  ASSETS: Fetcher;
}

const app = new Hono<{ Bindings: Env }>();

/** The only error shape this Worker produces: `{"error":{"code","message"}}`. */
function errorBody(code: string, message: string) {
  return { error: { code, message } };
}

app.get("/healthz", (c) =>
  // Liveness only: the process answers. Whether the index is usable is /readyz's question.
  c.json({ ok: true, as_of: new Date().toISOString() }),
);

app.get("/readyz", async (c) => {
  // The edge cannot run the Go verifier, so readiness is the one thing it can honestly check: the manifest
  // the board starts from is reachable. A bucket that is down and a bucket that is empty both fail here,
  // which is the right answer for a load balancer and the wrong one for a human — hence the `detail`.
  const base = c.env.DATA_BASE;
  if (!base) {
    return c.json({ ...errorBody("not_configured", "DATA_BASE is not set on this Worker"), ok: false }, 503);
  }
  try {
    const res = await fetch(new URL("/manifest.json", base), {
      method: "GET",
      headers: { "cache-control": "no-cache" },
      signal: AbortSignal.timeout(5000),
    });
    if (!res.ok) {
      return c.json({ ok: false, detail: `manifest ${res.status}` }, 503);
    }
    const manifest = (await res.json()) as { format?: string; towns?: unknown[] };
    return c.json({
      ok: true,
      format: manifest.format ?? "unknown",
      towns: Array.isArray(manifest.towns) ? manifest.towns.length : 0,
    });
  } catch (err) {
    return c.json({ ok: false, detail: err instanceof Error ? err.message : String(err) }, 503);
  }
});

// The five JSON routes from plan §5.6, plus the calendar feed. Until P5 they answer honestly rather than
// pretending to be a 404: the board can tell "not mine" from "not yet".
app.all("/api/v1/*", (c) =>
  c.json(
    errorBody(
      "not_implemented",
      `${new URL(c.req.url).pathname} arrives with the taste providers (P5); this Worker serves the index only`,
    ),
    501,
  ),
);

app.all("/calendar/*", (c) =>
  c.json(
    errorBody(
      "not_implemented",
      `${new URL(c.req.url).pathname} arrives with the taste providers (P5); this Worker serves the index only`,
    ),
    501,
  ),
);

app.notFound((c) => {
  // Assets are served before the script, so reaching this means the path is neither a file nor an API route. Two
  // different things can be true: a document route the SPA renders (`/a/<key>`), or a path that does not exist.
  // Hand the request back to the asset layer and let its `single-page-application` rule decide — it answers with
  // index.html for a client route and with the 404 page for a path that is genuinely nothing.
  const method = c.req.method;
  if (method === "GET" || method === "HEAD") {
    return c.env.ASSETS.fetch(c.req.raw);
  }
  return c.json(errorBody("not_found", "no such route"), 404);
});

app.onError((err, c) => {
  console.error("worker error", err instanceof Error ? err.stack : err);
  return c.json(errorBody("internal", "something broke"), 500);
});

export default app;
