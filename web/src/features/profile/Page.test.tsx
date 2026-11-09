import { describe, it } from "vitest";
import { expectStubPage } from "../../test/render";
import { ProfilePage } from "./Page";

describe("ProfilePage", () => {
  it("renders its head and an empty body at its route", () => {
    expectStubPage(ProfilePage, "profile");
  });
});
