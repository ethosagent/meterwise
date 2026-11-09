import { useEffect, useSyncExternalStore } from "react";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { Theme } from "../contract/generated";
import type { ResolvedMode } from "./theme";

export const THEME_STORAGE_KEY = "ui.theme";
const DARK_QUERY = "(prefers-color-scheme: dark)";
const MOTION_QUERY = "(prefers-reduced-motion: reduce)";

interface ThemeState {
  mode: Theme;
  setMode: (mode: Theme) => void;
}

// Storage can throw (private windows, blocked site data): fall back to memory only.
const safeStorage = createJSONStorage<{ mode: Theme }>(() => ({
  getItem: (k) => {
    try {
      return localStorage.getItem(k);
    } catch {
      return null;
    }
  },
  setItem: (k, v) => {
    try {
      localStorage.setItem(k, v);
    } catch {
      // not persisted; the in-memory choice still applies
    }
  },
  removeItem: (k) => {
    try {
      localStorage.removeItem(k);
    } catch {
      // nothing to remove
    }
  },
}));

/** UI-state slice for the theme choice, persisted to localStorage. */
export const useTheme = create<ThemeState>()(
  persist((set) => ({ mode: "system", setMode: (mode) => set({ mode }) }), {
    name: THEME_STORAGE_KEY,
    storage: safeStorage,
    partialize: (s) => ({ mode: s.mode }),
  }),
);

/** True once the user (or a server sync) has stored an explicit choice on this device. */
export function hasStoredTheme(): boolean {
  try {
    return localStorage.getItem(THEME_STORAGE_KEY) !== null;
  } catch {
    return false;
  }
}

function subscribeMedia(query: string) {
  return (notify: () => void) => {
    const mql = window.matchMedia(query);
    mql.addEventListener("change", notify);
    return () => mql.removeEventListener("change", notify);
  };
}

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    subscribeMedia(query),
    () => window.matchMedia(query).matches,
    () => false,
  );
}

/** The mode to render: an explicit choice wins, "system" follows prefers-color-scheme. */
export function useResolvedMode(): ResolvedMode {
  const mode = useTheme((s) => s.mode);
  const systemDark = useMedia(DARK_QUERY);
  if (mode === "system") return systemDark ? "dark" : "light";
  return mode;
}

export function usePrefersReducedMotion(): boolean {
  return useMedia(MOTION_QUERY);
}

/** Mirrors the resolved mode onto <html> so the browser's own widgets match. */
export function useApplyModeToDocument(mode: ResolvedMode): void {
  useEffect(() => {
    document.documentElement.dataset.theme = mode;
    document.documentElement.style.colorScheme = mode;
  }, [mode]);
}
