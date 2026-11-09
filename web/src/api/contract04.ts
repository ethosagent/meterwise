// Plan 04 wire types, hand-written to match .superpowers/sdd/00-overview/04-interface.md exactly.
// INTEGRATION: the backend regenerates contract/generated.ts with these. When it does, delete this file
// and import the same names from "../contract/generated" (Workspace then carries the three nullable
// fields, so WorkspaceView becomes Workspace). Nothing else changes.
import type { Workspace } from "../contract/generated";

/**
 * A Workspace as the API now sends it: publicId, proxyUrl and allowedClientCidrs are null when the
 * caller lacks workspace_url:read on that workspace, in every response that carries a Workspace.
 */
export type WorkspaceView = Omit<Workspace, "publicId" | "proxyUrl"> & {
  publicId: string | null;
  proxyUrl: string | null;
  allowedClientCidrs: string[] | null;
};

export interface SetupSnippets {
  openaiEnv: string;
  anthropicEnv: string;
  opencodeJson: string;
  claudeCodeEnv: string;
  curl: string;
}

export interface SetupStatus {
  firstCallAt: string | null;
  lastCallAt: string | null;
  callsToday: number;
}

/** GET /api/workspaces/{id}/setup */
export interface Setup {
  proxyUrl: string;
  snippets: SetupSnippets;
  status: SetupStatus;
}

/** POST /api/workspaces/{id}/url/reset */
export interface ResetUrlResult {
  proxyUrl: string;
  previousGraceUntil: string;
}

export interface AuditActor {
  userId: string;
  email: string;
  name: string;
}

export interface AuditEntry {
  id: string;
  at: string;
  actor: AuditActor | null;
  action: string;
  resourceType: string;
  resourceId: string | null;
  workspaceId: string | null;
  ip: string | null;
  metadata: Record<string, unknown>;
}

/** GET /api/audit-log */
export interface AuditPage {
  items: AuditEntry[];
  nextCursor: string | null;
}

/** The fixed action list; an unknown action filter is a 422. */
export const AUDIT_ACTIONS = [
  "workspace.create",
  "workspace.update",
  "workspace.delete",
  "workspace.url_reset",
  "workspace.url_retire_previous",
  "workspace.allowed_cidrs_update",
  "invitation.create",
  "invitation.resend",
  "invitation.revoke",
  "member.role_change",
  "member.remove",
  "group.create",
  "group.update",
  "group.delete",
  "group.permissions_update",
  "group.workspace_attach",
  "group.workspace_detach",
  "group.member_add",
  "group.member_remove",
  "group.members_update",
  "org.permissions_update",
] as const;

export type AuditAction = (typeof AUDIT_ACTIONS)[number];

export const GRACE_MINUTES_MAX = 1440;
export const GRACE_MINUTES_DEFAULT = 10;
export const ALLOWED_CIDRS_MAX = 32;
