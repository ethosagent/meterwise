import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Preferences, PreferencesPatch } from "../contract/generated";
import { apiFetch } from "./client";

export const preferencesKey = ["me", "preferences"] as const;

export const DEFAULT_PREFERENCES: Preferences = { theme: "system", reducedMotion: false };

export function useServerPreferences() {
  return useQuery({
    queryKey: preferencesKey,
    queryFn: () => apiFetch<Preferences>("/api/me/preferences"),
    staleTime: Number.POSITIVE_INFINITY,
  });
}

/** PATCH only the fields that changed: the server merges, so an unrelated stored field is never overwritten. */
export function useSavePreferences() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (next: PreferencesPatch) =>
      apiFetch<Preferences>("/api/me/preferences", { method: "PATCH", body: JSON.stringify(next) }),
    onSuccess: (saved) => qc.setQueryData(preferencesKey, saved),
  });
}
