import { useSession } from "../auth/session";
import { toApiError } from "./errors";

/**
 * JSON fetch against the same-origin API; throws ApiError on any non-2xx.
 * This is the one place the bearer token is attached and the one place a 401 is
 * turned into "session expired".
 */
export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("accept", "application/json");
  if (init.body !== undefined) headers.set("content-type", "application/json");
  const token = await useSession.getState().getToken();
  if (token) headers.set("authorization", `Bearer ${token}`);
  const res = await fetch(path, { ...init, headers });
  // A 401 with no token sent is just "not signed in yet"; only a rejected token means expiry.
  if (res.status === 401 && token) useSession.getState().markExpired();
  if (!res.ok) throw await toApiError(res);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
