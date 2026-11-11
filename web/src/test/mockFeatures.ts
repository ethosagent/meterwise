import type {
  MemoryConfigPatch,
  MemorySettings,
  RouterConfigPatch,
  RouterSettings,
  RouterStats,
  TrimConfig,
  TrimConfigPatch,
} from "../contract/generated";
import { MEMORY_SETTINGS } from "./fixtures/memory";
import { ROUTER_SETTINGS, ROUTER_STATS } from "./fixtures/router";
import { TRIM_CONFIG } from "./fixtures/trim";
import { type AccessMock, forbidden, jsonResponse as json } from "./mockAccess";

export interface FeaturesInit {
  trim?: TrimConfig;
  router?: RouterSettings;
  routerStats?: RouterStats;
  memory?: MemorySettings;
}

export interface FeaturesMock {
  trim: () => TrimConfig;
  router: () => RouterSettings;
  /** The verdict set on a call through POST .../router-feedback (undefined: never set). */
  verdictOf: (callId: string) => string | null | undefined;
  memory: () => MemorySettings;
  route: (method: string, path: string, body: Record<string, unknown>) => Response | undefined;
}

/**
 * The trim, router and memory settings routes over an in-memory state, with the permission rows of the
 * contract: monitor_config:read for GET, monitor_config:write for PATCH. A PATCH merges the fields it names.
 */
export function createFeaturesMock(access: AccessMock, init: FeaturesInit = {}): FeaturesMock {
  let trim = structuredClone(init.trim ?? TRIM_CONFIG);
  let router = structuredClone(init.router ?? ROUTER_SETTINGS);
  const routerStats = structuredClone(init.routerStats ?? ROUTER_STATS);
  let memory = structuredClone(init.memory ?? MEMORY_SETTINGS);

  const verdicts = new Map<string, string | null>();

  const route = (method: string, rawPath: string, body: Record<string, unknown>) => {
    const path = rawPath.split("?")[0] ?? "";
    const stats = /^\/api\/workspaces\/([^/]+)\/router\/stats$/.exec(path);
    if (stats && method === "GET") {
      if (!access.can("savings", "read", stats[1] ?? "")) return forbidden("no matching grant");
      return json({ ...routerStats, mode: router.config.mode });
    }
    const fb = /^\/api\/workspaces\/([^/]+)\/calls\/([^/]+)\/router-feedback$/.exec(path);
    if (fb && method === "POST") {
      if (!access.can("session", "read", fb[1] ?? "")) return forbidden("no matching grant");
      const verdict = (body.verdict as string | null | undefined) ?? null;
      verdicts.set(decodeURIComponent(fb[2] ?? ""), verdict);
      return json({ callId: decodeURIComponent(fb[2] ?? ""), verdict });
    }
    const m = /^\/api\/workspaces\/([^/?]+)\/(trim|router|memory)$/.exec(
      rawPath.split("?")[0] ?? "",
    );
    if (!m || (method !== "GET" && method !== "PATCH")) return undefined;
    const [, wsId = "", feature = ""] = m;
    if (!access.can("monitor_config", method === "GET" ? "read" : "write", wsId)) {
      return forbidden("no matching grant");
    }
    if (feature === "trim") {
      if (method === "PATCH") {
        const p = body as TrimConfigPatch;
        trim = { ...trim, ...p, stages: { ...trim.stages, ...p.stages } };
      }
      return json(trim);
    }
    if (feature === "router") {
      if (method === "PATCH") {
        const { resetMap, ...p } = body as RouterConfigPatch;
        const map = resetMap ? null : (p.map ?? router.config.map);
        router = { ...router, config: { ...router.config, ...p, map } };
        router.effective = map ?? router.defaults;
      }
      return json(router);
    }
    if (method === "PATCH") {
      const p = body as MemoryConfigPatch;
      memory = { ...memory, config: { ...memory.config, ...p } };
    }
    return json(memory);
  };

  return {
    trim: () => trim,
    router: () => router,
    verdictOf: (id) => verdicts.get(id),
    memory: () => memory,
    route,
  };
}
