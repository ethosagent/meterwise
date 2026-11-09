import { describe, it } from "vitest";
import { expectStubPage } from "../../test/render";
import { SetupPage } from "./Page";

describe("SetupPage", () => {
  it("renders its head and an empty body at its route", () => {
    expectStubPage(SetupPage, "setup");
  });
});
