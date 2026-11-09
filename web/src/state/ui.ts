import { create } from "zustand";

interface UiState {
  siderCollapsed: boolean;
  setSiderCollapsed: (collapsed: boolean) => void;
}

/** Ephemeral UI state. Server state, including the active workspace, lives in TanStack Query. */
export const useUi = create<UiState>((set) => ({
  siderCollapsed: false,
  setSiderCollapsed: (siderCollapsed) => set({ siderCollapsed }),
}));
