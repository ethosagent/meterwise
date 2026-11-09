import { type ResourceType, TAXONOMY } from "../access/taxonomy";
import type { WireErrorBody } from "../api/errors";
import type { Grant, Group, Member, PermissionSnapshot } from "../contract/generated";

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export function errorResponse(
  status: number,
  code: string,
  message: string,
  details: Record<string, unknown> = {},
): Response {
  const body: WireErrorBody = { code, message, details };
  return jsonResponse(body, status);
}

const forbidden = (reason: "no matching grant" | "admin required") =>
  errorResponse(403, "forbidden", "not allowed", { reason });

const READS: Grant[] = (
  ["workspace", "workspace_url", "session", "monitor_config", "savings"] as ResourceType[]
).map((resourceType) => ({ workspaceId: null, resourceType, action: "read" }));

/** What the seed gives a member: read on every workspace-scoped resource, in every workspace. */
export const MEMBER_SNAPSHOT: PermissionSnapshot = { admin: false, grants: READS };
export const ADMIN_SNAPSHOT: PermissionSnapshot = { admin: true, grants: [] };

export const ADA: Member = {
  userId: "u1",
  email: "ada@acme.test",
  name: "Ada Admin",
  avatarUrl: "",
  role: "admin",
  groupIds: [],
  joinedAt: "2026-11-01T00:00:00Z",
};
export const BO: Member = {
  ...ADA,
  userId: "u2",
  email: "bo@acme.test",
  name: "Bo Member",
  role: "member",
};

export interface MockGroup extends Group {
  grants: Grant[];
  memberIds: string[];
}

export interface AccessInit {
  snapshot?: PermissionSnapshot;
  members?: Member[];
  groups?: MockGroup[];
  /** The organization defaults (default: the seed, read on every workspace-scoped resource). */
  orgDefaults?: Grant[];
}

export interface AccessMock {
  members: Member[];
  groups: MockGroup[];
  snapshot: PermissionSnapshot;
  orgDefaults: Grant[];
  setSnapshot: (s: PermissionSnapshot) => void;
  /** The gateway's rule (taxonomy scope included), written independently of the production hasPermission. */
  can: (resource: ResourceType, action: string, workspaceId?: string) => boolean;
  isAdmin: () => boolean;
  route: (method: string, path: string, body: Record<string, unknown>) => Response | undefined;
}

let seq = 0;

/** A small in-memory stand-in for the Plan 03 endpoints, enforcing the interface file's route table. */
export function createAccessMock(admin: boolean, init: AccessInit = {}): AccessMock {
  const m: AccessMock = {
    members: structuredClone(init.members ?? [ADA, BO]),
    groups: structuredClone(init.groups ?? []),
    orgDefaults: structuredClone(init.orgDefaults ?? READS),
    snapshot: init.snapshot ?? (admin ? ADMIN_SNAPSHOT : MEMBER_SNAPSHOT),
    setSnapshot: (s) => {
      m.snapshot = s;
    },
    can: (resource, action, workspaceId) => {
      if (m.snapshot.admin) return true;
      const spec = TAXONOMY[resource];
      if (!(spec.actions as readonly string[]).includes(action)) return false;
      return m.snapshot.grants.some(
        (g) =>
          g.resourceType === resource &&
          g.action === action &&
          // The taxonomy's scope rule: an org resource needs an org-wide grant, a workspace resource
          // matches an org-wide grant or one pinned to that workspace.
          (g.workspaceId === null || (spec.scope === "workspace" && g.workspaceId === workspaceId)),
      );
    },
    isAdmin: () => m.snapshot.admin,
    route: (method, path, body) => handle(m, method, path, body),
  };
  return m;
}

const view = (g: MockGroup): Group => ({
  id: g.id,
  name: g.name,
  memberCount: g.memberIds.length,
  workspaceIds: g.workspaceIds,
  createdAt: g.createdAt,
});

function handle(m: AccessMock, method: string, path: string, body: Record<string, unknown>) {
  if (path === "/api/me/permissions") return jsonResponse(m.snapshot);
  if (path === "/api/organization/permissions") return orgDefaults(m, method, body);
  const mem = /^\/api\/members(?:\/([^/]+))?$/.exec(path);
  if (mem) return members(m, method, mem[1], body);
  const grp =
    /^\/api\/groups(?:\/([^/]+)(?:\/(permissions|workspaces|members)(?:\/([^/]+))?)?)?$/.exec(path);
  if (grp) return groups(m, method, { id: grp[1], sub: grp[2], arg: grp[3] }, body);
  return undefined;
}

function members(
  m: AccessMock,
  method: string,
  id: string | undefined,
  body: Record<string, unknown>,
) {
  if (method === "GET") {
    return m.can("member", "read")
      ? jsonResponse({ items: m.members })
      : forbidden("no matching grant");
  }
  if (!m.isAdmin()) return forbidden("admin required");
  const target = m.members.find((x) => x.userId === id);
  if (!target) return errorResponse(404, "not_found", "no such member");
  const admins = m.members.filter((x) => x.role === "admin").length;
  const lastAdmin = target.role === "admin" && admins === 1;
  if (method === "PATCH") {
    if (lastAdmin && body.role !== "admin") return errorResponse(409, "last_admin", "last admin");
    target.role = body.role as Member["role"];
    return jsonResponse(target);
  }
  if (lastAdmin) return errorResponse(409, "last_admin", "last admin");
  m.members = m.members.filter((x) => x !== target);
  for (const g of m.groups) g.memberIds = g.memberIds.filter((u) => u !== id);
  return new Response(null, { status: 204 });
}

interface GroupRoute {
  id?: string | undefined;
  sub?: string | undefined;
  arg?: string | undefined;
}

function groups(m: AccessMock, method: string, r: GroupRoute, body: Record<string, unknown>) {
  const reading = method === "GET";
  if (reading && !m.can("member", "read")) return forbidden("no matching grant");
  if (!reading && !m.isAdmin()) return forbidden("admin required");
  if (!r.id) {
    if (reading) return jsonResponse({ items: m.groups.map(view) });
    const name = String(body.name ?? "").trim();
    if (!name) return errorResponse(422, "validation_failed", "name required");
    if (m.groups.some((g) => g.name.toLowerCase() === name.toLowerCase())) {
      return errorResponse(409, "group_name_taken", "name taken");
    }
    const g: MockGroup = {
      id: `grp_${++seq}`,
      name,
      memberCount: 0,
      workspaceIds: [],
      createdAt: "2026-11-09T00:00:00Z",
      grants: [],
      memberIds: [],
    };
    m.groups = [...m.groups, g];
    return jsonResponse(view(g), 201);
  }
  const g = m.groups.find((x) => x.id === r.id);
  if (!g) return errorResponse(404, "not_found", "no such group");
  if (!r.sub) {
    if (reading) return jsonResponse(view(g));
    if (method === "DELETE") {
      m.groups = m.groups.filter((x) => x !== g);
      return new Response(null, { status: 204 });
    }
    const name = String(body.name ?? "").trim();
    if (m.groups.some((x) => x !== g && x.name.toLowerCase() === name.toLowerCase())) {
      return errorResponse(409, "group_name_taken", "name taken");
    }
    g.name = name;
    return jsonResponse(view(g));
  }
  if (r.sub === "permissions") {
    if (!reading) g.grants = body.grants as Grant[];
    return jsonResponse({ grants: g.grants });
  }
  if (r.sub === "workspaces") {
    if (method === "POST") {
      const ws = String(body.workspaceId);
      if (!g.workspaceIds.includes(ws)) g.workspaceIds = [...g.workspaceIds, ws];
      const seeded: Grant[] = (
        ["workspace", "workspace_url", "session", "monitor_config", "savings"] as ResourceType[]
      ).map((resourceType) => ({ workspaceId: ws, resourceType, action: "read" }));
      g.grants = [...g.grants.filter((x) => x.workspaceId !== ws), ...seeded];
      return jsonResponse(view(g));
    }
    g.workspaceIds = g.workspaceIds.filter((w) => w !== r.arg);
    g.grants = g.grants.filter((x) => x.workspaceId !== r.arg);
    return new Response(null, { status: 204 });
  }
  if (method === "PUT") {
    const ids = body.userIds as string[];
    if (ids.some((id) => !m.members.some((x) => x.userId === id))) {
      return errorResponse(422, "validation_failed", "every id must be an active member");
    }
    g.memberIds = [...new Set(ids)];
  }
  if (method === "POST") {
    const userId = String(body.userId);
    if (!m.members.some((x) => x.userId === userId))
      return errorResponse(422, "validation_failed", "not a member");
    if (!g.memberIds.includes(userId)) g.memberIds = [...g.memberIds, userId];
  }
  if (method === "DELETE") {
    g.memberIds = g.memberIds.filter((u) => u !== r.arg);
    return new Response(null, { status: 204 });
  }
  return jsonResponse({ items: m.members.filter((x) => g.memberIds.includes(x.userId)) });
}

function orgDefaults(m: AccessMock, method: string, body: Record<string, unknown>) {
  if (method === "GET") {
    return m.can("member", "read")
      ? jsonResponse({ grants: m.orgDefaults })
      : forbidden("no matching grant");
  }
  if (!m.isAdmin()) return forbidden("admin required");
  const grants = body.grants as Grant[];
  if (grants.some((g) => g.workspaceId !== null)) {
    return errorResponse(422, "validation_failed", "org defaults must use a null workspace");
  }
  m.orgDefaults = grants;
  return jsonResponse({ grants });
}
