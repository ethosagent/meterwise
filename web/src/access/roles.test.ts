import { describe, expect, it } from "vitest";
import { ROLE_OPTIONS } from "./roles";

describe("ROLE_OPTIONS", () => {
  it("lists member then admin", () => {
    expect(ROLE_OPTIONS.map((o) => o.value)).toEqual(["member", "admin"]);
  });
});
