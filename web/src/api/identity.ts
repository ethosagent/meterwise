import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import type { ErrorCode, Invitation, Me, Role, Workspace } from "../contract/generated";
import { apiFetch } from "./client";
import { ApiError } from "./errors";
import { enc, json, useInvalidatingMutation } from "./mutation";

/** Error codes the identity screens branch on; `satisfies` ties each to the generated registry. */
export const IDENTITY_ERRORS = {
  notInvited: "not_invited",
  workspaceLimit: "workspace_limit",
  defaultWorkspace: "default_workspace",
  duplicateInvitation: "duplicate_invitation",
} as const satisfies Record<string, ErrorCode>;

export const meKey = ["me"] as const;
export const workspacesKey = ["workspaces"] as const;
const WORKSPACES_STALE_MS = 10_000;
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
    // The shell remounts the routed page on a workspace switch; that remount is not a reason to refetch the list.
    staleTime: WORKSPACES_STALE_MS,
  });
}

/**
 * The active workspace, never a stand-in for it. "missing" means the id in Me is not in the
 * workspace list even after one refetch of both: show an error, not the default workspace's name
 * or proxy URL (the URL is the workspace, so a wrong one routes an agent to the wrong place).
 * "none" means the server says the caller can read no workspace (activeWorkspaceId is null).
 */
export type ActiveWorkspace =
  | { state: "loading" }
  | { state: "none" }
  | { state: "missing" }
  | { state: "ready"; workspace: Workspace };

export function useActiveWorkspace(): ActiveWorkspace & { retry: () => void } {
  const meQuery = useMe();
  const listQuery = useWorkspaces();
  const activeId = meQuery.data?.activeWorkspaceId ?? undefined;
  const workspace = listQuery.data?.find((w) => w.id === activeId);
  const loaded = meQuery.data !== undefined && listQuery.data !== undefined;
  const absent = loaded && activeId !== undefined && workspace === undefined;
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
  if (meQuery.data && meQuery.data.activeWorkspaceId === null) return { state: "none", retry };
  if (!loaded || fetching || refetchedFor.current !== activeId) return { state: "loading", retry };
  return { state: "missing", retry };
}

export function useCreateWorkspace() {
  return useInvalidatingMutation(
    (name: string) =>
      apiFetch<Workspace>("/api/workspaces", { method: "POST", body: json({ name }) }),
    [workspacesKey],
  );
}

export function useRenameWorkspace() {
  return useInvalidatingMutation(
    (v: { id: string; name: string }) =>
      apiFetch<Workspace>(`/api/workspaces/${enc(v.id)}`, {
        method: "PATCH",
        body: json({ name: v.name }),
      }),
    [workspacesKey],
  );
}

export function useDeleteWorkspace() {
  // The server re-points a deleted active workspace, so Me and everything workspace-scoped are stale too.
  return useInvalidatingMutation(
    (id: string) => apiFetch<void>(`/api/workspaces/${enc(id)}`, { method: "DELETE" }),
    [workspacesKey, meKey, [WORKSPACE_SCOPE]],
  );
}

/** Switch the active workspace, then refetch everything that was scoped to the old one. */
export function useSwitchWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<{ activeWorkspaceId: string }>(`/api/workspaces/${enc(id)}/switch`, {
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

export function useInvite() {
  return useInvalidatingMutation(
    (v: { email: string; role: Role }) =>
      apiFetch<Invitation>("/api/invitations", { method: "POST", body: json(v) }),
    [invitationsKey],
  );
}

export function useResendInvitation() {
  return useInvalidatingMutation(
    (id: string) => apiFetch<Invitation>(`/api/invitations/${enc(id)}/resend`, { method: "POST" }),
    [invitationsKey],
  );
}

export function useRevokeInvitation() {
  return useInvalidatingMutation(
    (id: string) => apiFetch<void>(`/api/invitations/${enc(id)}`, { method: "DELETE" }),
    [invitationsKey],
  );
}
