import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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

export const permissionsKey = ["permissions"] as const;
export const membersKey = ["members"] as const;
export const groupsKey = ["groups"] as const;
export const orgPermissionsKey = ["organization", "permissions"] as const;

const enc = encodeURIComponent;

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

/** A mutation that refreshes the given keys and then the caller's own access. */
function useAccessMutation<V, R>(
  fn: (v: V) => Promise<R>,
  keys: readonly (readonly unknown[])[],
  /** Refresh on failure too, for a mutation that sends several requests and can half-apply. */
  settled = false,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    [settled ? "onSettled" : "onSuccess"]: async () => {
      await Promise.all([
        ...keys.map((queryKey) => qc.invalidateQueries({ queryKey })),
        refreshAccess(qc),
      ]);
    },
  });
}

const body = (v: unknown) => JSON.stringify(v);

export function useCreateGroup() {
  return useAccessMutation(
    (name: string) => apiFetch<Group>("/api/groups", { method: "POST", body: body({ name }) }),
    [groupsKey],
  );
}

export function useRenameGroup() {
  return useAccessMutation(
    (v: { id: string; name: string }) =>
      apiFetch<Group>(`/api/groups/${enc(v.id)}`, {
        method: "PATCH",
        body: body({ name: v.name }),
      }),
    [groupsKey],
  );
}

export function useDeleteGroup() {
  return useAccessMutation(
    (id: string) => apiFetch<void>(`/api/groups/${enc(id)}`, { method: "DELETE" }),
    [groupsKey, membersKey],
  );
}

export function usePutGroupGrants() {
  return useAccessMutation(
    (v: { id: string; grants: Grant[] }) =>
      apiFetch<{ grants: Grant[] }>(`/api/groups/${enc(v.id)}/permissions`, {
        method: "PUT",
        body: body({ grants: v.grants }),
      }),
    [groupsKey],
  );
}

/** Attach or detach a workspace. Attaching seeds read grants and detaching drops the group's grants there. */
export function useGroupWorkspace() {
  return useAccessMutation(
    async (v: { id: string; workspaceId: string; attach: boolean }) => {
      if (v.attach) {
        await apiFetch<Group>(`/api/groups/${enc(v.id)}/workspaces`, {
          method: "POST",
          body: body({ workspaceId: v.workspaceId }),
        });
      } else {
        await apiFetch<void>(`/api/groups/${enc(v.id)}/workspaces/${enc(v.workspaceId)}`, {
          method: "DELETE",
        });
      }
    },
    [groupsKey],
  );
}

/** Moves users in or out of a group, one request each, in order. Group membership touches group_users only. */
export function useGroupMembership() {
  return useAccessMutation(
    async (v: { id: string; add: string[]; remove: string[] }) => {
      for (const userId of v.add) {
        await apiFetch(`/api/groups/${enc(v.id)}/members`, {
          method: "POST",
          body: body({ userId }),
        });
      }
      for (const userId of v.remove) {
        await apiFetch(`/api/groups/${enc(v.id)}/members/${enc(userId)}`, { method: "DELETE" });
      }
    },
    [groupsKey, membersKey],
    true,
  );
}

export function usePutOrgDefaults() {
  return useAccessMutation(
    (grants: Grant[]) =>
      apiFetch<{ grants: Grant[] }>("/api/organization/permissions", {
        method: "PUT",
        body: body({ grants }),
      }),
    [orgPermissionsKey],
  );
}

export function useSetMemberRole() {
  return useAccessMutation(
    (v: { userId: string; role: Role }) =>
      apiFetch<Member>(`/api/members/${enc(v.userId)}`, {
        method: "PATCH",
        body: body({ role: v.role }),
      }),
    [membersKey],
  );
}

export function useRemoveMember() {
  return useAccessMutation(
    (userId: string) => apiFetch<void>(`/api/members/${enc(userId)}`, { method: "DELETE" }),
    [membersKey, groupsKey],
  );
}
