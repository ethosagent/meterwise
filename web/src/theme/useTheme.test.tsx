import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { installMatchMedia } from "../test/setup";
import { hasStoredTheme, THEME_STORAGE_KEY, useResolvedMode, useTheme } from "./useTheme";

beforeEach(() => {
  useTheme.setState({ mode: "system" });
  localStorage.clear();
  installMatchMedia();
});

describe("useTheme", () => {
  it("starts on system and has no stored choice", () => {
    expect(useTheme.getState().mode).toBe("system");
    expect(hasStoredTheme()).toBe(false);
  });

  it("persists an explicit choice to localStorage", () => {
    act(() => useTheme.getState().setMode("dark"));
    expect(hasStoredTheme()).toBe(true);
    expect(JSON.parse(localStorage.getItem(THEME_STORAGE_KEY) ?? "{}").state.mode).toBe("dark");
  });

  it("restores a stored choice on rehydrate", async () => {
    localStorage.setItem(
      THEME_STORAGE_KEY,
      JSON.stringify({ state: { mode: "light" }, version: 0 }),
    );
    await useTheme.persist.rehydrate();
    expect(useTheme.getState().mode).toBe("light");
  });

  it("survives storage that throws", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(() => act(() => useTheme.getState().setMode("dark"))).not.toThrow();
    expect(hasStoredTheme()).toBe(false);
    vi.restoreAllMocks();
  });
});

describe("useResolvedMode", () => {
  it("follows prefers-color-scheme while on system", () => {
    installMatchMedia((q) => q.includes("prefers-color-scheme: dark"));
    expect(renderHook(() => useResolvedMode()).result.current).toBe("dark");
    installMatchMedia(() => false);
    expect(renderHook(() => useResolvedMode()).result.current).toBe("light");
  });

  it("lets an explicit choice beat the system preference", () => {
    installMatchMedia((q) => q.includes("prefers-color-scheme: dark"));
    act(() => useTheme.getState().setMode("light"));
    expect(renderHook(() => useResolvedMode()).result.current).toBe("light");
  });
});
