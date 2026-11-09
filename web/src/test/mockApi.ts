import { vi } from "vitest";
import type { AppConfig, Preferences } from "../api/types";

export const TEST_CONFIG: AppConfig = { appName: "Acme Meter", proxyPublicHost: "proxy.acme.test" };

export interface MockApi {
  fetch: ReturnType<typeof vi.fn>;
  /** Parsed JSON bodies of every PATCH /api/me/preferences call. */
  patches: unknown[];
}

interface Options {
  config?: AppConfig | Response;
  preferences?: Preferences;
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

/** Stubs global fetch with the three Plan 01 endpoints. */
export function mockApi(opts: Options = {}): MockApi {
  let prefs: Preferences = opts.preferences ?? { theme: "system", reducedMotion: false };
  const patches: unknown[] = [];
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (url === "/api/config") {
      const c = opts.config ?? TEST_CONFIG;
      return c instanceof Response ? c.clone() : json(c);
    }
    if (url === "/api/me/preferences" && method === "GET") return json(prefs);
    if (url === "/api/me/preferences" && method === "PATCH") {
      const body = JSON.parse(String(init?.body)) as Preferences;
      patches.push(body);
      prefs = body;
      return json(prefs);
    }
    return json({ code: "not_found", message: "no such route", details: {} }, 404);
  });
  vi.stubGlobal("fetch", fn);
  return { fetch: fn, patches };
}

export function errorResponse(status: number, code: string, message: string): Response {
  return json({ code, message, details: {} }, status);
}
