import { create } from "zustand";

interface UiState {
  siderCollapsed: boolean;
  /** The create-workspace dialog is mounted by the Shell, outside the dark sider theme. */
  createWorkspaceOpen: boolean;
  setSiderCollapsed: (collapsed: boolean) => void;
  setCreateWorkspaceOpen: (open: boolean) => void;
}

/** Ephemeral UI state. Server state, including the active workspace, lives in TanStack Query. */
export const useUi = create<UiState>((set) => ({
  siderCollapsed: false,
  createWorkspaceOpen: false,
  setSiderCollapsed: (siderCollapsed) => set({ siderCollapsed }),
  setCreateWorkspaceOpen: (createWorkspaceOpen) => set({ createWorkspaceOpen }),
}));
