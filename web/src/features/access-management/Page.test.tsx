import { describe, it } from "vitest";
import { expectStubPage } from "../../test/render";
import { AccessManagementPage } from "./Page";

describe("AccessManagementPage", () => {
  it("renders its head and an empty body at its route", () => {
    expectStubPage(AccessManagementPage, "access");
  });
});
