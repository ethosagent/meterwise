// Plan 02 wire types, copied from the fixed interface (02-interface.md). The backend's generated
// contract will own these; when it lands, re-point the imports of this module and delete it.
import type { Preferences } from "../contract/generated";

export type Role = "admin" | "member";

export interface WorkspaceSettings {
  [key: string]: unknown;
}

export interface Workspace {
  id: string;
  publicId: string;
  name: string;
  isDefault: boolean;
  proxyUrl: string;
  settings: WorkspaceSettings;
  createdAt: string;
}

export interface MeUser {
  id: string;
  email: string;
  name: string;
  avatarUrl: string;
}

export interface Me {
  user: MeUser;
  orgId: string;
  role: Role;
  activeWorkspaceId: string;
  preferences: Preferences;
}

export type InvitationStatus = "pending" | "accepted" | "revoked" | "expired";

export interface Invitation {
  id: string;
  email: string;
  role: Role;
  status: InvitationStatus;
  invitedBy: string;
  expiresAt: string;
  createdAt: string;
}

/** Error codes this plan adds to the registry. */
export const IDENTITY_ERRORS = {
  notInvited: "not_invited",
  workspaceLimit: "workspace_limit",
  defaultWorkspace: "default_workspace",
  duplicateInvitation: "duplicate_invitation",
} as const;
