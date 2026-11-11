import type {
  MonitorConfig,
  MonitorConfigPatch,
  MonitorEvent,
  MonitorStats,
  SaveRow,
  SavingsCauses,
  SavingsOverview,
  Session,
  SessionDetail,
} from "../contract/generated";
import { CAUSES_RESPONSE, OVERVIEW_ACTIVE, SAVES } from "./fixtures/savings";
import { DETAIL, MONITOR_CONFIG, MONITOR_STATS, SESSIONS } from "./fixtures/sessions";
import { type AccessMock, errorResponse, forbidden, jsonResponse as json } from "./mockAccess";

export interface SavingsInit {
  overview?: SavingsOverview;
  causes?: SavingsCauses;
  saves?: SaveRow[];
  sessions?: Session[];
  details?: SessionDetail[];
  monitors?: MonitorConfig;
  stats?: MonitorStats;
}

export interface SavingsMock {
  /** The current monitor settings (PATCH merges into it). */
  monitors: () => MonitorConfig;
  /** The current feedback on a monitor event, by id. */
  feedbackOf: (eventId: string) => string | null | undefined;
  route: (method: string, path: string, body: Record<string, unknown>) => Response | undefined;
}

/**
 * The Plan 07 endpoints over an in-memory state, enforcing the interface file's permission rows:
 * savings:read for the three savings routes and monitors/stats, session:read for sessions and
 * feedback, monitor_config:read|write for the settings.
 */
export function createSavingsMock(access: AccessMock, init: SavingsInit = {}): SavingsMock {
  const overview = init.overview ?? OVERVIEW_ACTIVE;
  const causes = init.causes ?? CAUSES_RESPONSE;
  const saves = init.saves ?? SAVES;
  const sessions = init.sessions ?? SESSIONS;
  const details = structuredClone(init.details ?? [DETAIL]);
  const stats = init.stats ?? MONITOR_STATS;
  let monitors = structuredClone(init.monitors ?? MONITOR_CONFIG);

  const events = () => details.flatMap((d) => d.monitorEvents);
  const find = (id: string): MonitorEvent | undefined => events().find((e) => e.id === id);

  const route = (method: string, rawPath: string, body: Record<string, unknown>) => {
    const url = new URL(rawPath, "http://mock");
    const q = url.searchParams;
    const ws = /^\/api\/workspaces\/([^/]+)\/(.+)$/.exec(url.pathname);
    const det = /^\/api\/sessions\/([^/]+)$/.exec(url.pathname);
    if (det && method === "GET") {
      const wsId = q.get("workspaceId") ?? "";
      if (!access.can("session", "read", wsId)) return forbidden("no matching grant");
      const found = details.find((d) => d.session.id === det[1]);
      return found ? json(found) : errorResponse(404, "not_found", "no such session");
    }
    if (!ws) return undefined;
    const [, wsId = "", rest = ""] = ws;
    const need = (resource: "savings" | "session" | "monitor_config", action: string) =>
      access.can(resource, action, wsId) ? undefined : forbidden("no matching grant");

    if (method === "GET" && rest === "savings/overview") {
      return need("savings", "read") ?? json(overview);
    }
    if (method === "GET" && rest === "savings/causes") {
      return need("savings", "read") ?? json(causes);
    }
    if (method === "GET" && rest === "savings/saves") {
      const refused = need("savings", "read");
      if (refused) return refused;
      const limit = Number(q.get("limit") ?? 20);
      const start = Number(q.get("cursor") ?? 0);
      const items = saves.slice(start, start + limit);
      const next = start + limit < saves.length ? String(start + limit) : null;
      return json({ items, nextCursor: next });
    }
    if (method === "GET" && rest === "sessions") {
      const refused = need("session", "read");
      if (refused) return refused;
      const client = q.get("client");
      const outcome = q.get("outcome");
      const tag = q.get("tag");
      const tagged = (s: (typeof sessions)[number]) =>
        !tag ||
        (tag === "trimmed" && s.trimTokens > 0) ||
        (tag === "routed" && s.routedCalls > 0) ||
        (tag === "remembered" && s.hints > 0);
      const items = sessions.filter(
        (s) =>
          (!client || s.client === client) && (!outcome || s.outcome === outcome) && tagged(s),
      );
      return json({ items, nextCursor: null });
    }
    if (method === "GET" && rest === "monitors/stats") {
      return need("savings", "read") ?? json({ ...stats, range: q.get("range") ?? "7d" });
    }
    if (method === "GET" && rest === "monitors") {
      return need("monitor_config", "read") ?? json(monitors);
    }
    if (method === "PATCH" && rest === "monitors") {
      const refused = need("monitor_config", "write");
      if (refused) return refused;
      const patch = body as MonitorConfigPatch;
      const next = structuredClone(monitors) as unknown as Record<string, Record<string, unknown>>;
      for (const [name, part] of Object.entries(patch)) {
        next[name] = { ...next[name], ...part };
      }
      monitors = next as unknown as MonitorConfig;
      return json(monitors);
    }
    const fb = /^monitor-events\/([^/]+)\/feedback$/.exec(rest);
    if (method === "POST" && fb) {
      const refused = need("session", "read");
      if (refused) return refused;
      const ev = find(fb[1] ?? "");
      if (!ev) return errorResponse(404, "not_found", "no such event");
      ev.feedback = (body.feedback as string | null | undefined) ?? null;
      return json(ev);
    }
    return undefined;
  };

  return {
    monitors: () => monitors,
    feedbackOf: (id) => find(id)?.feedback,
    route,
  };
}
