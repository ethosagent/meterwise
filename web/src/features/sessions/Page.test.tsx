import { describe, it } from "vitest";
import { expectStubPage } from "../../test/render";
import { SessionsPage } from "./Page";

describe("SessionsPage", () => {
  it("renders its head and an empty body at its route", () => {
    expectStubPage(SessionsPage, "sessions");
  });
});
