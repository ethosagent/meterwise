import { describe, expect, it } from "vitest";
import { ALL_PAGES, DEFAULT_PATH, MAIN_PAGES, pageByKey, pageByPath, SETTINGS_PAGES } from "./nav";

describe("nav", () => {
  it("lists the planned destinations in order", () => {
    expect(MAIN_PAGES.map((p) => p.label)).toEqual(["Overview", "Sessions", "Monitors", "Setup"]);
    expect(SETTINGS_PAGES.map((p) => p.label)).toEqual([
      "Profile",
      "Workspace",
      "Organization",
      "Access management",
    ]);
  });

  it("has unique keys and paths, all absolute", () => {
    expect(new Set(ALL_PAGES.map((p) => p.key)).size).toBe(ALL_PAGES.length);
    expect(new Set(ALL_PAGES.map((p) => p.path)).size).toBe(ALL_PAGES.length);
    for (const p of ALL_PAGES) expect(p.path.startsWith("/")).toBe(true);
  });

  it("puts the settings pages under /settings and groups them", () => {
    for (const p of SETTINGS_PAGES) {
      expect(p.path.startsWith("/settings/")).toBe(true);
      expect(p.group).toBe("Settings");
    }
  });

  it("looks pages up by key and path, and defaults to the first", () => {
    expect(pageByKey("access").label).toBe("Access management");
    expect(pageByPath("/sessions")?.key).toBe("sessions");
    expect(pageByPath("/nope")).toBeUndefined();
    expect(DEFAULT_PATH).toBe("/overview");
  });
});
