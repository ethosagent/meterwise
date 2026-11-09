/**
 * TEMPORARY. The Go half of Plan 03 generates src/contract/generated.ts (Member, Group, Grant,
 * PermissionSnapshot, Me with a nullable activeWorkspaceId, the new error codes) and
 * src/access/taxonomy.ts. Neither is on this branch yet, so the shapes from
 * .superpowers/sdd/00-overview/03-interface.md live here, with a hand-written copy of the taxonomy.
 * The integration step deletes this file and repoints its importers at the generated modules.
 */
import type { Me as GeneratedMe } from "../contract/generated";

/** Plan 03 section 4: resource type to its actions and its fixed scope. */
export const TAXONOMY = {
  workspace: { scope: "workspace", actions: ["read", "write", "manage"] },
  workspace_url: { scope: "workspace", actions: ["read", "manage"] },
  session: { scope: "workspace", actions: ["read"] },
  monitor_config: { scope: "workspace", actions: ["read", "write"] },
  savings: { scope: "workspace", actions: ["read"] },
  member: { scope: "org", actions: ["read", "manage"] },
  audit_log: { scope: "org", actions: ["read"] },
} as const;

export type ResourceType = keyof typeof TAXONOMY;
export type Scope = (typeof TAXONOMY)[ResourceType]["scope"];
export type Action = (typeof TAXONOMY)[ResourceType]["actions"][number];

/** "resource:action", as in the interface file. */
export type Permission = {
  [R in ResourceType]: `${R}:${(typeof TAXONOMY)[R]["actions"][number]}`;
}[ResourceType];

export const RESOURCE_TYPES = Object.keys(TAXONOMY) as ResourceType[];

export interface Grant {
  workspaceId: string | null;
  resourceType: ResourceType;
  action: Action;
}

export interface PermissionSnapshot {
  admin: boolean;
  grants: Grant[];
}

/** Me.activeWorkspaceId becomes nullable: null when the caller can read no workspace. */
export type Me = Omit<GeneratedMe, "activeWorkspaceId"> & { activeWorkspaceId: string | null };

export interface Member {
  userId: string;
  email: string;
  name: string;
  avatarUrl: string;
  role: "admin" | "member";
  groupIds: string[];
  joinedAt: string;
}

export interface Group {
  id: string;
  name: string;
  memberCount: number;
  workspaceIds: string[];
  createdAt: string;
}

/** New error codes (interface file). Plain strings until the generated registry has them. */
export const ACCESS_ERRORS = {
  lastAdmin: "last_admin",
  alreadyMember: "already_member",
  groupNameTaken: "group_name_taken",
} as const;
