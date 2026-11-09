import { describe, expect, it } from "vitest";
import { errorResponse, mockApi } from "../test/mockApi";
import { apiFetch } from "./client";
import { ApiError } from "./errors";

describe("apiFetch", () => {
  it("returns parsed JSON and asks for JSON", async () => {
    const api = mockApi();
    const cfg = await apiFetch<{ appName: string }>("/api/config");
    expect(cfg.appName).toBe("Acme Meter");
    const init = api.fetch.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).get("accept")).toBe("application/json");
  });

  it("sends a JSON content type with a body", async () => {
    const api = mockApi();
    await apiFetch("/api/me/preferences", {
      method: "PATCH",
      body: JSON.stringify({ theme: "dark", reducedMotion: true }),
    });
    const init = api.fetch.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).get("content-type")).toBe("application/json");
    expect(api.patches).toEqual([{ theme: "dark", reducedMotion: true }]);
  });

  it("throws ApiError carrying the registry code on failure", async () => {
    mockApi({ config: errorResponse(500, "internal", "boom") });
    await expect(apiFetch("/api/config")).rejects.toMatchObject({
      name: "ApiError",
      code: "internal",
      status: 500,
    });
    await expect(apiFetch("/api/nope")).rejects.toBeInstanceOf(ApiError);
  });
});
