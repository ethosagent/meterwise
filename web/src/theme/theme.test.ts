import { theme } from "antd";
import { describe, expect, it } from "vitest";
import { darkTheme, lightTheme, siderTheme, themeFor } from "./theme";

describe("theme configs", () => {
  it("uses the light algorithm for light and the dark one for dark", () => {
    expect(lightTheme.algorithm).toBe(theme.defaultAlgorithm);
    expect(darkTheme.algorithm).toBe(theme.darkAlgorithm);
  });

  it("gives the two modes different surfaces and the same radius", () => {
    expect(lightTheme.token?.colorBgContainer).not.toBe(darkTheme.token?.colorBgContainer);
    expect(lightTheme.token?.borderRadius).toBe(darkTheme.token?.borderRadius);
  });

  it("keeps the sider dark with the same surface in both modes", () => {
    expect(siderTheme.algorithm).toBe(theme.darkAlgorithm);
    expect(lightTheme.components?.Layout?.siderBg).toBe(darkTheme.components?.Layout?.siderBg);
    expect(siderTheme.components?.Layout?.siderBg).toBe(lightTheme.components?.Layout?.siderBg);
  });

  it("turns animation off for reduced motion only", () => {
    expect(themeFor("dark", true).token?.motion).toBe(false);
    expect(themeFor("dark", false)).toBe(darkTheme);
    expect(themeFor("light", false)).toBe(lightTheme);
  });
});
