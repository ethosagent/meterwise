import { describe, expect, it } from "vitest";
import {
  formatBp,
  formatTokens,
  formatUsd,
  isPositiveMicro,
  isZeroMicro,
  maxMicro,
  microToNumber,
  parseMicro,
  sharePct,
  tryParseMicro,
} from "./money";

describe("money", () => {
  it.each([
    ["1.000000", "$1.00"],
    ["0.500000", "$0.50"],
    ["0.000000", "$0.00"],
    ["1234567.890000", "$1,234,567.89"],
    ["0.004999", "$0.004999"],
    ["0.001500", "$0.0015"],
    ["0.000001", "$0.000001"],
    ["0.010000", "$0.01"],
    ["0.005000", "$0.0050"],
    ["0.014999", "$0.01"],
    ["0.015000", "$0.02"],
    ["-2.500000", "-$2.50"],
    ["9007199254740993.000000", "$9,007,199,254,740,993.00"],
    ["3", "$3.00"],
  ])("formats %s as %s", (wire, shown) => {
    expect(formatUsd(wire)).toBe(shown);
  });

  it("parses exactly and rejects anything that is not a decimal", () => {
    expect(parseMicro("1.500000")).toBe(1_500_000n);
    expect(parseMicro("-0.000250")).toBe(-250n);
    expect(parseMicro("12")).toBe(12_000_000n);
    for (const bad of ["", "abc", "1.2345678", "1e3", "+1", " 1"]) {
      expect(tryParseMicro(bad)).toBeNull();
    }
    expect(() => parseMicro("x")).toThrow(/not a money amount/);
  });

  it("answers zero and positive", () => {
    expect(isZeroMicro("0.000000")).toBe(true);
    expect(isZeroMicro("0.000001")).toBe(false);
    expect(isPositiveMicro("0.000001")).toBe(true);
    expect(isPositiveMicro("-1.000000")).toBe(false);
  });

  it("computes shares and maxima without floats", () => {
    expect(sharePct("1.000000", "4.000000")).toBe(25);
    expect(sharePct("1.000000", "0.000000")).toBe(0);
    expect(sharePct("-1.000000", "4.000000")).toBe(0);
    expect(maxMicro(["0.5", "2.25", "1"])).toBe("2.250000");
    expect(maxMicro([])).toBe("0.000000");
  });

  it("formats basis points and tokens", () => {
    expect(formatBp(1000)).toBe("10%");
    expect(formatBp(1234)).toBe("12.34%");
    expect(formatBp(1205)).toBe("12.05%");
    expect(formatBp(0)).toBe("0%");
    expect(formatBp(10000)).toBe("100%");
    expect(formatBp(null)).toBe("n/a");
    expect(formatTokens(120000)).toBe("120,000");
    expect(formatTokens(999)).toBe("999");
  });

  it("converts to a chart coordinate", () => {
    expect(microToNumber("1.500000")).toBe(1.5);
  });
});
