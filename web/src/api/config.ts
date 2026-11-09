import { useQuery } from "@tanstack/react-query";
import type { AppConfig } from "../contract/generated";
import { apiFetch } from "./client";

export const configKey = ["config"] as const;

/** The deployment's public config (brand name, proxy host). Static for a session. */
export function useConfig() {
  return useQuery({
    queryKey: configKey,
    queryFn: () => apiFetch<AppConfig>("/api/config"),
    staleTime: Number.POSITIVE_INFINITY,
  });
}
