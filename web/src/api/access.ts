import { type QueryClient, useQuery } from "@tanstack/react-query";
import type {
  ErrorCode,
  Grant,
  Group,
  Member,
  PermissionSnapshot,
  Role,
} from "../contract/generated";
import { apiFetch } from "./client";
import { meKey, workspacesKey } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";

export const permissionsKey = ["permissions"] as const;
export const membersKey = ["members"] as const;
export const groupsKey = ["groups"] as const;
export const orgPermissionsKey = ["organization", "permissions"] as const;

/** Readable lines for the Plan 03 conflicts; pass as `byCode` to describeApiError. */
export const ACCESS_ERROR_TEXT = {
  last_admin:
    "That would leave the organization without an admin. Make someone else an admin first.",
  already_member: "That person is already a member of the organization.",
  group_name_taken: "A group with that name already exists. Pick another name.",
  forbidden: "You do not have permission to do that.",
} as const satisfies Partial<Record<ErrorCode, string>>;

/**
 * Anything that can change what the caller may do (a grant, a group, a role) or which workspaces
 * they can read. Also run on a 403: the snapshot the UI held was stale.
 */
export function refreshAccess(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: permissionsKey }),
    qc.invalidateQueries({ queryKey: workspacesKey }),
    qc.invalidateQueries({ queryKey: meKey }),
  ]);
}

/** The caller's flattened grants, or admin:true. Refetched on any access change and on a 403. */
export function usePermissionSnapshot(enabled = true) {
  return useQuery({
    queryKey: permissionsKey,
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    queryFn: () => apiFetch<PermissionSnapshot>("/api/me/permissions"),
  });
}

export function useMembers(enabled: boolean) {
  return useQuery({
    queryKey: membersKey,
    enabled,
    queryFn: async () => (await apiFetch<{ items: Member[] }>("/api/members")).items,
  });
}

export function useGroups(enabled: boolean) {
  return useQuery({
    queryKey: groupsKey,
    enabled,
    queryFn: async () => (await apiFetch<{ items: Group[] }>("/api/groups")).items,
  });
}

export function useGroupGrants(groupId: string) {
  return useQuery({
    queryKey: [...groupsKey, groupId, "permissions"],
    queryFn: async () =>
      (await apiFetch<{ grants: Grant[] }>(`/api/groups/${enc(groupId)}/permissions`)).grants,
  });
}

export function useGroupMembers(groupId: string) {
  return useQuery({
    queryKey: [...groupsKey, groupId, "members"],
    queryFn: async () =>
      (await apiFetch<{ items: Member[] }>(`/api/groups/${enc(groupId)}/members`)).items,
  });
}

/** The organization defaults: grants that apply to every member. A null workspace means every workspace. */
export function useOrgDefaults(enabled: boolean) {
  return useQuery({
    queryKey: orgPermissionsKey,
    enabled,
    queryFn: async () =>
      (await apiFetch<{ grants: Grant[] }>("/api/organization/permissions")).grants,
  });
}

/**
 * What a change to a grant, a role, a removed member, a deleted group or an attached workspace can
 * alter for the caller: their access, workspace list and Me. Creating or renaming a group, or moving
 * members between groups, does not, so those refresh the group lists only.
 */
const ACCESS_KEYS = [permissionsKey, workspacesKey, meKey] as const;

export function useCreateGroup() {
  return useInvalidatingMutation(
    (name: string) => apiFetch<Group>("/api/groups", { method: "POST", body: json({ name }) }),
    [groupsKey],
  );
}

export function useRenameGroup() {
  return useInvalidatingMutation(
    (v: { id: string; name: string }) =>
      apiFetch<Group>(`/api/groups/${enc(v.id)}`, {
        method: "PATCH",
        body: json({ name: v.name }),
      }),
    [groupsKey],
  );
}

export function useDeleteGroup() {
  return useInvalidatingMutation(
    (id: string) => apiFetch<void>(`/api/groups/${enc(id)}`, { method: "DELETE" }),
    [groupsKey, membersKey, ...ACCESS_KEYS],
  );
}

export function usePutGroupGrants() {
  return useInvalidatingMutation(
    (v: { id: string; grants: Grant[] }) =>
      apiFetch<{ grants: Grant[] }>(`/api/groups/${enc(v.id)}/permissions`, {
        method: "PUT",
        body: json({ grants: v.grants }),
      }),
    [groupsKey, ...ACCESS_KEYS],
  );
}

/** Attaching seeds read grants for the group on the workspace. */
export function useAttachWorkspace() {
  return useInvalidatingMutation(
    (v: { id: string; workspaceId: string }) =>
      apiFetch<Group>(`/api/groups/${enc(v.id)}/workspaces`, {
        method: "POST",
        body: json({ workspaceId: v.workspaceId }),
      }),
    [groupsKey, ...ACCESS_KEYS],
  );
}

/** Detaching drops every grant the group has on the workspace. */
export function useDetachWorkspace() {
  return useInvalidatingMutation(
    (v: { id: string; workspaceId: string }) =>
      apiFetch<void>(`/api/groups/${enc(v.id)}/workspaces/${enc(v.workspaceId)}`, {
        method: "DELETE",
      }),
    [groupsKey, ...ACCESS_KEYS],
  );
}

/** Moves users in or out of a group, one request each, in order. Group membership touches group_users only. */
export function useGroupMembership() {
  return useInvalidatingMutation(
    async (v: { id: string; add: string[]; remove: string[] }) => {
      for (const userId of v.add) {
        await apiFetch(`/api/groups/${enc(v.id)}/members`, {
          method: "POST",
          body: json({ userId }),
        });
      }
      for (const userId of v.remove) {
        await apiFetch(`/api/groups/${enc(v.id)}/members/${enc(userId)}`, { method: "DELETE" });
      }
    },
    [groupsKey, membersKey],
    { settled: true },
  );
}

export function usePutOrgDefaults() {
  return useInvalidatingMutation(
    (grants: Grant[]) =>
      apiFetch<{ grants: Grant[] }>("/api/organization/permissions", {
        method: "PUT",
        body: json({ grants }),
      }),
    [orgPermissionsKey, ...ACCESS_KEYS],
  );
}

export function useSetMemberRole() {
  return useInvalidatingMutation(
    (v: { userId: string; role: Role }) =>
      apiFetch<Member>(`/api/members/${enc(v.userId)}`, {
        method: "PATCH",
        body: json({ role: v.role }),
      }),
    [membersKey, ...ACCESS_KEYS],
  );
}

export function useRemoveMember() {
  return useInvalidatingMutation(
    (userId: string) => apiFetch<void>(`/api/members/${enc(userId)}`, { method: "DELETE" }),
    [membersKey, groupsKey, ...ACCESS_KEYS],
  );
}
