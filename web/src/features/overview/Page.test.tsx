import { describe, it } from "vitest";
import { expectStubPage } from "../../test/render";
import { OverviewPage } from "./Page";

describe("OverviewPage", () => {
  it("renders its head and an empty body at its route", () => {
    expectStubPage(OverviewPage, "overview");
  });
});
