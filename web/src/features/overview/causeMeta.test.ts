import { describe, expect, it } from "vitest";
import { SAVINGS_CAUSES } from "../../contract/generated";
import { causeLabel, modeLabel } from "./causeMeta";

describe("causeMeta", () => {
  it("labels every one of the nine causes", () => {
    expect(SAVINGS_CAUSES).toHaveLength(9);
    for (const c of SAVINGS_CAUSES) expect(causeLabel(c)).not.toBe("");
    expect(causeLabel("trim")).toBe("Trimming");
    expect(causeLabel("router")).toBe("Model routing");
    expect(causeLabel("memory")).toBe("Memory");
    expect(causeLabel("duplicate_tool_call")).toBe("Duplicate tool call");
  });

  it("names the saver modes", () => {
    expect(modeLabel("off")).toBe("Off");
    expect(modeLabel("lossless")).toBe("Lossless");
    expect(modeLabel("hint")).toBe("Hint");
  });
});
