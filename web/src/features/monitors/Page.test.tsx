import { describe, it } from "vitest";
import { expectStubPage } from "../../test/render";
import { MonitorsPage } from "./Page";

describe("MonitorsPage", () => {
  it("renders its head and an empty body at its route", () => {
    expectStubPage(MonitorsPage, "monitors");
  });
});
