import { describe, expect, it } from "vitest";
import { relativeTime } from "./relative";

const NOW = new Date("2026-11-10T12:00:00Z");

describe("relativeTime", () => {
  it.each([
    ["2026-11-10T11:59:40Z", "just now"],
    ["2026-11-10T11:58:00Z", "2 minutes ago"],
    ["2026-11-10T09:00:00Z", "3 hours ago"],
    ["2026-11-08T12:00:00Z", "2 days ago"],
    ["2026-11-10T12:00:30Z", "just now"],
  ])("%s is %s", (iso, want) => {
    expect(relativeTime(iso, NOW)).toBe(want);
  });
});
