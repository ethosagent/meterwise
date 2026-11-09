import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";
import { ApiError } from "./errors";
import {
  IDENTITY_ERRORS,
  type Invitation,
  type Me,
  type Role,
  type Workspace,
} from "./identity-types";

export const meKey = ["me"] as const;
export const workspacesKey = ["workspaces"] as const;
export const invitationsKey = ["invitations"] as const;

/** Every query whose data depends on the active workspace is keyed under this root, so a switch can invalidate them all. */
export const WORKSPACE_SCOPE = "ws" as const;
export function workspaceScoped<T extends readonly unknown[]>(...parts: T) {
  return [WORKSPACE_SCOPE, ...parts] as const;
}

function invalidateWorkspaceScoped(qc: QueryClient) {
  return qc.invalidateQueries({ queryKey: [WORKSPACE_SCOPE] });
}

/**
 * The signed-in user. First login: GET /me says not_invited until the invitation is claimed, so
 * try POST /auth/complete-signup once; if that is refused too, the person was never invited.
 */
async function fetchMe(): Promise<Me> {
  try {
    return await apiFetch<Me>("/api/me");
  } catch (e) {
    if (e instanceof ApiError && e.code === IDENTITY_ERRORS.notInvited) {
      return apiFetch<Me>("/api/auth/complete-signup", { method: "POST" });
    }
    throw e;
  }
}

export function useMe(enabled = true) {
  return useQuery({
    queryKey: meKey,
    queryFn: fetchMe,
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    retry: false,
  });
}

export function useWorkspaces() {
  return useQuery({
    queryKey: workspacesKey,
    queryFn: async () => (await apiFetch<{ items: Workspace[] }>("/api/workspaces")).items,
  });
}

/** The workspace `Me.activeWorkspaceId` points at, once both have loaded. */
export function useActiveWorkspace(): Workspace | undefined {
  const me = useMe().data;
  const list = useWorkspaces().data;
  return list?.find((w) => w.id === me?.activeWorkspaceId) ?? list?.find((w) => w.isDefault);
}

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      apiFetch<Workspace>("/api/workspaces", { method: "POST", body: JSON.stringify({ name }) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: workspacesKey }),
  });
}

export function useRenameWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; name: string }) =>
      apiFetch<Workspace>(`/api/workspaces/${encodeURIComponent(v.id)}`, {
        method: "PATCH",
        body: JSON.stringify({ name: v.name }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: workspacesKey }),
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<void>(`/api/workspaces/${encodeURIComponent(id)}`, { method: "DELETE" }),
    onSuccess: async () => {
      // The server re-points a deleted active workspace, so both lists are stale.
      await Promise.all([
        qc.invalidateQueries({ queryKey: workspacesKey }),
        qc.invalidateQueries({ queryKey: meKey }),
      ]);
      await invalidateWorkspaceScoped(qc);
    },
  });
}

/** Switch the active workspace, then refetch everything that was scoped to the old one. */
export function useSwitchWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<{ activeWorkspaceId: string }>(`/api/workspaces/${encodeURIComponent(id)}/switch`, {
        method: "POST",
      }),
    onSuccess: async ({ activeWorkspaceId }) => {
      qc.setQueryData<Me>(meKey, (me) => (me ? { ...me, activeWorkspaceId } : me));
      await invalidateWorkspaceScoped(qc);
    },
  });
}

export function useInvitations(enabled: boolean) {
  return useQuery({
    queryKey: invitationsKey,
    enabled,
    queryFn: async () => (await apiFetch<{ items: Invitation[] }>("/api/invitations")).items,
  });
}

function useInvitationMutation<V>(fn: (v: V) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: invitationsKey }),
  });
}

export function useInvite() {
  return useInvitationMutation((v: { email: string; role: Role }) =>
    apiFetch<Invitation>("/api/invitations", { method: "POST", body: JSON.stringify(v) }),
  );
}

export function useResendInvitation() {
  return useInvitationMutation((id: string) =>
    apiFetch<Invitation>(`/api/invitations/${encodeURIComponent(id)}/resend`, { method: "POST" }),
  );
}

export function useRevokeInvitation() {
  return useInvitationMutation((id: string) =>
    apiFetch<void>(`/api/invitations/${encodeURIComponent(id)}`, { method: "DELETE" }),
  );
}
