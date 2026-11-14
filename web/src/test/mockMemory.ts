import { vi } from "vitest";
import type { MemoryStats, TraceDetail, TraceSummary } from "../contract/generated";
import { MEMORY_STATS, TRACE_DETAIL, TRACES } from "./fixtures/memory";
import { MEMORY_HIT } from "./fixtures/sessions";
import { type AccessMock, errorResponse, forbidden, jsonResponse as json } from "./mockAccess";

export interface MemoryInit {
  stats?: MemoryStats;
  traces?: TraceSummary[];
  detail?: TraceDetail;
  /** Server-side cap of a page of GET .../traces (the cursor is the offset); default 2, so three traces make two pages. The client's `limit` also caps it. */
  pageSize?: number;
}

export interface MemoryMock {
  /** The feedback set on a hit through POST .../memory-hits/{id}/feedback (undefined: never set). */
  feedbackOf: (hitId: string) => string | null | undefined;
  traces: () => TraceSummary[];
  route: (method: string, path: string, body: Record<string, unknown>) => Response | undefined;
}

/**
 * The memory read routes over an in-memory state, with the permission rows of the contract: stats savings:read,
 * traces and hit feedback session:read, "Forget memory" (DELETE traces) workspace:manage.
 */
export function createMemoryMock(access: AccessMock, init: MemoryInit = {}): MemoryMock {
  const stats = structuredClone(init.stats ?? MEMORY_STATS);
  let traces = structuredClone(init.traces ?? TRACES);
  const detail = structuredClone(init.detail ?? TRACE_DETAIL);
  const cap = init.pageSize ?? 2;
  const feedback = new Map<string, string | null>();

  const route = (method: string, rawPath: string, body: Record<string, unknown>) => {
    const [path = "", query = ""] = rawPath.split("?");
    const m =
      /^\/api\/workspaces\/([^/]+)\/(memory\/stats|traces|memory-hits\/([^/]+)\/feedback)(?:\/([^/]+))?$/.exec(
        path,
      );
    if (!m) return undefined;
    const ws = m[1] ?? "";
    const [, , what = "", hitId, traceId] = m;
    if (what === "memory/stats" && method === "GET") {
      if (!access.can("savings", "read", ws)) return forbidden("no matching grant");
      return json(stats);
    }
    if (what === "traces" && !traceId && method === "GET") {
      if (!access.can("session", "read", ws)) return forbidden("no matching grant");
      const q = new URLSearchParams(query);
      const from = Number(q.get("cursor") ?? "0");
      const size = Math.min(cap, Number(q.get("limit") ?? cap));
      const next = from + size;
      return json({
        items: traces.slice(from, next),
        nextCursor: next < traces.length ? String(next) : null,
      });
    }
    if (what === "traces" && !traceId && method === "DELETE") {
      if (!access.can("workspace", "manage", ws)) return forbidden("no matching grant");
      traces = [];
      return new Response(null, { status: 204 });
    }
    if (what === "traces" && traceId && method === "GET") {
      if (!access.can("session", "read", ws)) return forbidden("no matching grant");
      const t = traces.find((x) => x.id === decodeURIComponent(traceId));
      return t
        ? json({ ...detail, id: t.id, sessionId: t.sessionId })
        : errorResponse(404, "not_found", "no such trace");
    }
    if (hitId && method === "POST") {
      if (!access.can("session", "read", ws)) return forbidden("no matching grant");
      const id = decodeURIComponent(hitId);
      if (id !== MEMORY_HIT.id) return errorResponse(404, "not_found", "no such hit");
      const fb = (body.feedback as string | null | undefined) ?? null;
      feedback.set(id, fb);
      return json({ ...MEMORY_HIT, feedback: fb });
    }
    return undefined;
  };
  return { feedbackOf: (id) => feedback.get(id), traces: () => traces, route };
}

/** From now on every request with this method and a path matching `pattern` answers 500; the rest go on as before. */
export function failRoute(method: string, pattern: RegExp) {
  const next = globalThis.fetch;
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) =>
    (init?.method ?? "GET") === method && pattern.test(String(input))
      ? Promise.resolve(errorResponse(500, "internal", "down"))
      : next(input, init),
  );
}
