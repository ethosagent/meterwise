import { describe, expect, it } from "vitest";
import { DETAIL, SAVER_DETAIL, call } from "../../test/fixtures/sessions";
import { saverTags, stepEvents } from "./stepEvents";

const callOf = (n: number) => {
  const c = SAVER_DETAIL.calls.find((x) => x.callIndex === n);
  if (!c) throw new Error(`no call ${n}`);
  return c;
};

describe("stepEvents", () => {
  it("returns one kind per saver, in hook order", () => {
    expect(stepEvents(SAVER_DETAIL, callOf(11)).map((e) => e.kind)).toEqual(["trim"]);
    expect(stepEvents(SAVER_DETAIL, callOf(14)).map((e) => e.kind)).toEqual(["route"]);
    expect(stepEvents(SAVER_DETAIL, callOf(16)).map((e) => e.kind)).toEqual(["memory_hint"]);
  });

  it("a trimmed and routed call lists trim before route; monitor fires follow", () => {
    const both = call(20, { trimAction: "acted", routerAction: "routed" });
    const detail = { ...DETAIL, calls: [both], monitorEvents: [] };
    expect(stepEvents(detail, both).map((e) => e.kind)).toEqual(["trim", "route"]);
    expect(stepEvents(DETAIL, DETAIL.calls[2] as never).map((e) => e.kind)).toEqual([
      "monitor_fire",
    ]);
  });

  it("a plain call has no events and no tags", () => {
    expect(stepEvents(DETAIL, DETAIL.calls[0] as never)).toEqual([]);
    expect(saverTags(call(1), false)).toEqual([]);
  });

  it("tags show acted trims and routes only, never an observe", () => {
    expect(saverTags(callOf(11), false)).toEqual(["Trimmed −1,000 tokens"]);
    expect(saverTags(callOf(13), false)).toEqual([]);
    expect(saverTags(callOf(14), false)).toEqual(["Routed → gpt-4.1-mini"]);
    expect(saverTags(callOf(15), false)).toEqual([]);
    expect(saverTags(callOf(16), true)).toEqual(["Remembered"]);
  });
});
