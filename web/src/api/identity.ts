import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import type { ErrorCode, Invitation, Me, Role, Workspace } from "../contract/generated";
import { apiFetch } from "./client";
import { ApiError } from "./errors";

/** Error codes the identity screens branch on; `satisfies` ties each to the generated registry. */
export const IDENTITY_ERRORS = {
  notInvited: "not_invited",
  workspaceLimit: "workspace_limit",
  defaultWorkspace: "default_workspace",
  duplicateInvitation: "duplicate_invitation",
} as const satisfies Record<string, ErrorCode>;

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

/**
 * The active workspace, never a stand-in for it. "missing" means the id in Me is not in the
 * workspace list even after one refetch of both: show an error, not the default workspace's name
 * or proxy URL (the URL is the workspace, so a wrong one routes an agent to the wrong place).
 */
export type ActiveWorkspace =
  | { state: "loading" }
  | { state: "missing" }
  | { state: "ready"; workspace: Workspace };

export function useActiveWorkspace(): ActiveWorkspace & { retry: () => void } {
  const meQuery = useMe();
  const listQuery = useWorkspaces();
  const activeId = meQuery.data?.activeWorkspaceId;
  const workspace = listQuery.data?.find((w) => w.id === activeId);
  const loaded = meQuery.data !== undefined && listQuery.data !== undefined;
  const absent = loaded && workspace === undefined;
  const fetching = meQuery.isFetching || listQuery.isFetching;
  const refetchedFor = useRef<string | null>(null);
  const { refetch: refetchMe } = meQuery;
  const { refetch: refetchList } = listQuery;

  useEffect(() => {
    if (!absent || fetching || refetchedFor.current === activeId) return;
    refetchedFor.current = activeId ?? null;
    void Promise.all([refetchMe(), refetchList()]);
  }, [absent, fetching, activeId, refetchMe, refetchList]);

  const retry = () => {
    refetchedFor.current = activeId ?? null;
    void Promise.all([refetchMe(), refetchList()]);
  };
  if (workspace) return { state: "ready", workspace, retry };
  if (!loaded || fetching || refetchedFor.current !== activeId) return { state: "loading", retry };
  return { state: "missing", retry };
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
