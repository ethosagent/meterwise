import { describe, expect, it } from "vitest";
import { parseRange, rangeBounds, rangeKey, rangeQuery, utcToday, validCustomRange } from "./range";

const TODAY = "2026-11-10";
const p = (q: string) => new URLSearchParams(q);

describe("range", () => {
  it("defaults to 7d and reads 30d", () => {
    expect(parseRange(p(""), TODAY)).toEqual({ range: "7d", from: "", to: "" });
    expect(parseRange(p("range=30d"), TODAY).range).toBe("30d");
    expect(parseRange(p("range=bogus"), TODAY).range).toBe("7d");
    expect(parseRange(p("range=7D"), TODAY).range).toBe("7d"); // case-sensitive, like the server
  });

  it.each([
    ["2026-11-01", "2026-11-05", true],
    ["2026-11-10", "2026-11-10", true],
    ["2026-11-05", "2026-11-01", false], // from after to
    ["2026-11-01", "2026-11-11", false], // after today
    ["2026-08-12", "2026-11-10", false], // 91 days
    ["2026-08-13", "2026-11-10", true], // exactly 90 days
    ["2026-11-1", "2026-11-05", false],
    ["2026-13-01", "2026-13-02", false],
    ["", "", false],
  ])("custom %s..%s valid=%s", (from, to, ok) => {
    expect(validCustomRange(from, to, TODAY)).toBe(ok);
  });

  it("keeps a valid custom range and drops an invalid one", () => {
    expect(parseRange(p("range=custom&from=2026-11-01&to=2026-11-05"), TODAY)).toEqual({
      range: "custom",
      from: "2026-11-01",
      to: "2026-11-05",
    });
    expect(parseRange(p("range=custom&from=2026-11-05&to=2026-11-01"), TODAY).range).toBe("7d");
    expect(parseRange(p("from=2026-11-01&to=2026-11-05"), TODAY).range).toBe("7d");
  });

  it("builds the query and a stable key", () => {
    expect(rangeQuery({ range: "30d", from: "", to: "" }).toString()).toBe("range=30d");
    expect(rangeQuery({ range: "custom", from: "2026-11-01", to: "2026-11-05" }).toString()).toBe(
      "range=custom&from=2026-11-01&to=2026-11-05",
    );
    expect(rangeKey({ range: "7d", from: "", to: "" })).toBe("7d");
    expect(rangeKey({ range: "custom", from: "a", to: "b" })).toBe("custom:a:b");
  });

  it("turns a selection into [from, to) bounds in UTC", () => {
    const now = new Date("2026-11-10T15:30:00Z");
    expect(rangeBounds({ range: "7d", from: "", to: "" }, now)).toEqual({
      from: "2026-11-04T00:00:00Z",
      to: "2026-11-11T00:00:00Z",
    });
    expect(rangeBounds({ range: "30d", from: "", to: "" }, now).from).toBe("2026-10-12T00:00:00Z");
    expect(rangeBounds({ range: "custom", from: "2026-11-01", to: "2026-11-05" }, now)).toEqual({
      from: "2026-11-01T00:00:00Z",
      to: "2026-11-06T00:00:00Z",
    });
    expect(utcToday(now)).toBe("2026-11-10");
  });
});
