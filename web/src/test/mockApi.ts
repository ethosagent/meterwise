import { vi } from "vitest";
import type {
  AppConfig,
  Invitation,
  Me,
  Preferences,
  PreferencesPatch,
  Role,
  Workspace,
} from "../contract/generated";
import {
  type AccessInit,
  type AccessMock,
  createAccessMock,
  errorResponse,
  jsonResponse as json,
} from "./mockAccess";

export { errorResponse };

export const TEST_CONFIG: AppConfig = { appName: "Acme Meter", proxyPublicHost: "proxy.acme.test" };

export const ADMIN_ME: Me = {
  user: { id: "u1", email: "ada@acme.test", name: "Ada Admin", avatarUrl: "" },
  orgId: "org1",
  role: "admin",
  activeWorkspaceId: "ws_1",
  preferences: { theme: "system", reducedMotion: false },
};
export const MEMBER_ME: Me = { ...ADMIN_ME, role: "member" as Role };

function workspace(n: number, name: string, isDefault = false): Workspace {
  const publicId = `pub${String(n).padStart(19, "0")}`;
  return {
    id: `ws_${n}`,
    publicId,
    name,
    isDefault,
    proxyUrl: `https://${TEST_CONFIG.proxyPublicHost}/w/${publicId}`,
    settings: {},
    createdAt: "2026-11-01T00:00:00Z",
  };
}

export const TEST_WORKSPACES: Workspace[] = [
  workspace(1, "Default workspace", true),
  workspace(2, "Staging"),
  workspace(3, "Research"),
];

export interface Call {
  method: string;
  path: string;
  body: unknown;
  authorization: string | null;
}

export interface MockApi {
  fetch: ReturnType<typeof vi.fn>;
  /** Parsed JSON bodies of every PATCH /api/me/preferences call. */
  patches: unknown[];
  calls: Call[];
  /** Every call whose method and path match. */
  callsTo: (method: string, path: string | RegExp) => Call[];
  /** The Plan 03 in-memory server: snapshot, members, groups. */
  access: AccessMock;
  /** From now on every authenticated route answers 401 (the session ended). */
  expireSession: () => void;
}

interface Options {
  config?: AppConfig | Response;
  preferences?: Preferences;
  /** Make GET /api/me/preferences fail with this status. */
  preferencesGetStatus?: number;
  /** Make PATCH /api/me/preferences fail with this status. */
  preferencesPatchStatus?: number;
  /** The signed-in user; null means nobody invited them (403 not_invited). */
  me?: Me | null;
  /** With me:null, whether POST /auth/complete-signup finds a pending invitation and creates the membership. */
  claimable?: boolean;
  workspaces?: Workspace[];
  invitations?: Invitation[];
  maxWorkspaces?: number;
  /** Workspaces the list only includes from its second fetch on (a stale list that a refetch repairs). */
  revealOnRefetch?: Workspace[];
  /** Plan 03 state: the caller's permission snapshot (default by role), members, groups. */
  access?: AccessInit;
}

/** Stubs global fetch with the Plan 01 and Plan 02 endpoints, backed by a small in-memory server. */
export function mockApi(opts: Options = {}): MockApi {
  let prefs: Preferences = opts.preferences ?? { theme: "system", reducedMotion: false };
  let me: Me | null = opts.me === undefined ? ADMIN_ME : opts.me;
  let workspaces = [...(opts.workspaces ?? TEST_WORKSPACES)];
  let invitations = [...(opts.invitations ?? [])];
  let nextId = 100;
  let expired = false;
  let listFetches = 0;
  const patches: unknown[] = [];
  const calls: Call[] = [];

  const access = createAccessMock(me?.role === "admin", opts.access);
  const notInvited = () => errorResponse(403, "not_invited", "no invitation");
  const notFound = () => errorResponse(404, "not_found", "no such thing");

  const route = (method: string, path: string, body: Record<string, unknown>): Response => {
    if (path === "/api/config") {
      const c = opts.config ?? TEST_CONFIG;
      return c instanceof Response ? c.clone() : json(c);
    }
    if (expired) return errorResponse(401, "unauthenticated", "token expired");
    if (path === "/api/me/preferences" && method === "GET") {
      return opts.preferencesGetStatus
        ? errorResponse(opts.preferencesGetStatus, "internal", "down")
        : json(prefs);
    }
    if (path === "/api/me/preferences" && method === "PATCH") {
      patches.push(body);
      if (opts.preferencesPatchStatus) {
        return errorResponse(opts.preferencesPatchStatus, "internal", "down");
      }
      prefs = { ...prefs, ...(body as PreferencesPatch) } as Preferences; // the server merges
      return json(prefs);
    }
    if (path === "/api/auth/complete-signup" && method === "POST") {
      if (me === null && opts.claimable) me = ADMIN_ME;
      return me ? json(me) : notInvited();
    }
    if (path === "/api/me") return me ? json(me) : notInvited();
    if (!me) return notInvited();
    const granted = access.route(method, path, body);
    if (granted) return granted;
    const ws = /^\/api\/workspaces\/([^/]+)(\/switch)?$/.exec(path);
    if (path === "/api/workspaces" && method === "GET") {
      listFetches++;
      const late = listFetches > 1 ? (opts.revealOnRefetch ?? []) : [];
      const all = [...workspaces, ...late].filter((w) => access.can("workspace", "read", w.id));
      return json({ items: all });
    }
    if (path === "/api/workspaces" && method === "POST") {
      if (workspaces.length >= (opts.maxWorkspaces ?? 20)) {
        return errorResponse(409, "workspace_limit", "workspace limit reached");
      }
      const created = workspace(nextId++, String(body.name));
      workspaces = [...workspaces, created];
      return json(created, 201);
    }
    if (ws) {
      const target = workspaces.find((w) => w.id === ws[1]);
      if (!target) return notFound();
      // The interface file: PATCH needs workspace:write, DELETE workspace:manage, switch workspace:read.
      const need = method === "PATCH" ? "write" : method === "DELETE" ? "manage" : "read";
      if (!access.can("workspace", need, target.id)) {
        return errorResponse(403, "forbidden", "not allowed", { reason: "no matching grant" });
      }
      if (ws[2] && method === "POST") {
        me = { ...me, activeWorkspaceId: target.id };
        return json({ activeWorkspaceId: target.id });
      }
      if (method === "PATCH") {
        const next = { ...target, name: String(body.name ?? target.name) };
        workspaces = workspaces.map((w) => (w.id === target.id ? next : w));
        return json(next);
      }
      if (method === "DELETE") {
        if (target.isDefault) return errorResponse(409, "default_workspace", "default workspace");
        workspaces = workspaces.filter((w) => w.id !== target.id);
        if (me.activeWorkspaceId === target.id) {
          me = { ...me, activeWorkspaceId: workspaces.find((w) => w.isDefault)?.id ?? "" };
        }
        return new Response(null, { status: 204 });
      }
    }
    if (path.startsWith("/api/invitations")) {
      if (!access.can("member", "manage")) {
        return errorResponse(403, "forbidden", "not allowed", { reason: "no matching grant" });
      }
      const inv = /^\/api\/invitations\/([^/]+)(\/resend)?$/.exec(path);
      if (path === "/api/invitations" && method === "GET") return json({ items: invitations });
      if (path === "/api/invitations" && method === "POST") {
        if (body.role === "admin" && !access.isAdmin()) {
          return errorResponse(403, "forbidden", "not allowed", { reason: "admin required" });
        }
        if (access.members.some((x) => x.email === body.email)) {
          return errorResponse(409, "already_member", "already a member");
        }
        if (invitations.some((i) => i.email === body.email && i.status === "pending")) {
          return errorResponse(409, "duplicate_invitation", "already invited");
        }
        const created: Invitation = {
          id: `inv_${nextId++}`,
          email: String(body.email),
          role: body.role as Role,
          status: "pending",
          invitedBy: me.user.id,
          expiresAt: "2026-12-01T00:00:00Z",
          createdAt: "2026-11-09T00:00:00Z",
        };
        invitations = [...invitations, created];
        return json(created, 201);
      }
      const found = inv && invitations.find((i) => i.id === inv[1]);
      if (!inv || !found) return notFound();
      if (inv[2] && method === "POST") return json(found);
      if (method === "DELETE") {
        invitations = invitations.filter((i) => i.id !== found.id);
        return new Response(null, { status: 204 });
      }
    }
    return notFound();
  };

  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    const method = init?.method ?? "GET";
    const body: Record<string, unknown> = init?.body ? JSON.parse(String(init.body)) : {};
    calls.push({
      method,
      path,
      body: init?.body ? body : undefined,
      authorization: new Headers(init?.headers).get("authorization"),
    });
    return route(method, path, body);
  });
  vi.stubGlobal("fetch", fn);
  return {
    fetch: fn,
    patches,
    access,
    calls,
    callsTo: (method, path) =>
      calls.filter(
        (c) =>
          c.method === method && (typeof path === "string" ? c.path === path : path.test(c.path)),
      ),
    expireSession: () => {
      expired = true;
    },
  };
}
