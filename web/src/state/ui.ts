import { create } from "zustand";

interface UiState {
  siderCollapsed: boolean;
  activeWorkspaceId: string;
  setSiderCollapsed: (collapsed: boolean) => void;
  setActiveWorkspaceId: (id: string) => void;
}

/** Ephemeral UI state. The workspace id is a stub until Plan 02 brings real tenancy. */
export const useUi = create<UiState>((set) => ({
  siderCollapsed: false,
  activeWorkspaceId: "ws-default",
  setSiderCollapsed: (siderCollapsed) => set({ siderCollapsed }),
  setActiveWorkspaceId: (activeWorkspaceId) => set({ activeWorkspaceId }),
}));
