import type {
  MemoryConfigPatch,
  MemorySettings,
  RouterConfigPatch,
  RouterSettings,
  TrimConfig,
  TrimConfigPatch,
} from "../contract/generated";
import { MEMORY_SETTINGS } from "./fixtures/memory";
import { ROUTER_SETTINGS } from "./fixtures/router";
import { TRIM_CONFIG } from "./fixtures/trim";
import { type AccessMock, forbidden, jsonResponse as json } from "./mockAccess";

export interface FeaturesInit {
  trim?: TrimConfig;
  router?: RouterSettings;
  memory?: MemorySettings;
}

export interface FeaturesMock {
  trim: () => TrimConfig;
  router: () => RouterSettings;
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
  let memory = structuredClone(init.memory ?? MEMORY_SETTINGS);

  const route = (method: string, rawPath: string, body: Record<string, unknown>) => {
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

  return { trim: () => trim, router: () => router, memory: () => memory, route };
}
