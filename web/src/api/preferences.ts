import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";
import type { Preferences } from "./types";

export const preferencesKey = ["me", "preferences"] as const;

export const DEFAULT_PREFERENCES: Preferences = { theme: "system", reducedMotion: false };

export function useServerPreferences() {
  return useQuery({
    queryKey: preferencesKey,
    queryFn: () => apiFetch<Preferences>("/api/me/preferences"),
    staleTime: Number.POSITIVE_INFINITY,
  });
}

/** PATCH the full preferences object so the server never has to guess missing fields. */
export function useSavePreferences() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (next: Preferences) =>
      apiFetch<Preferences>("/api/me/preferences", { method: "PATCH", body: JSON.stringify(next) }),
    onSuccess: (saved) => qc.setQueryData(preferencesKey, saved),
  });
}
