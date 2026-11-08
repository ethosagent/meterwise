import { describe, expect, it } from "vitest";
import { ERROR_CODES, ERROR_STATUS, ROUTES } from "./generated";

describe("generated contract twin", () => {
  it("has a status for every error code", () => {
    for (const code of ERROR_CODES) expect(ERROR_STATUS[code]).toBeGreaterThanOrEqual(400);
  });

  it("every non-public route names a permission", () => {
    for (const r of ROUTES) expect(r.public || r.permission !== "").toBe(true);
  });
});
