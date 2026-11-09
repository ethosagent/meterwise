import { describe, expect, it } from "vitest";
import { ApiError, describeError, toApiError } from "./errors";

describe("describeError", () => {
  it("describes known and unknown error codes", () => {
    expect(describeError("internal")).toMatch(/server/);
    expect(describeError("forbidden")).toMatch(/not accepted/);
    expect(describeError("made_up")).toBe("Something went wrong.");
  });
});

describe("toApiError", () => {
  it("keeps code, message, details and status from a registry body", async () => {
    const res = new Response(
      JSON.stringify({ code: "forbidden", message: "no", details: { need: "x" } }),
      { status: 403 },
    );
    const err = await toApiError(res);
    expect(err).toBeInstanceOf(ApiError);
    expect([err.status, err.code, err.message, err.details]).toEqual([
      403,
      "forbidden",
      "no",
      { need: "x" },
    ]);
  });

  it("reads the problem list from details.problems and ignores other shapes", async () => {
    const mk = (details: unknown) =>
      toApiError(
        new Response(JSON.stringify({ code: "invalid_request", message: "m", details }), {
          status: 400,
        }),
      );
    expect((await mk({ problems: ["theme: must be one of", "body: too large"] })).problems).toEqual(
      ["theme: must be one of", "body: too large"],
    );
    expect((await mk({ problems: "nope" })).problems).toEqual([]);
    expect((await mk(["legacy"])).problems).toEqual([]);
    expect((await mk({})).problems).toEqual([]);
  });

  it("defaults missing details to an empty object", async () => {
    const res = new Response(JSON.stringify({ code: "c", message: "m" }), { status: 400 });
    expect((await toApiError(res)).details).toEqual({});
  });

  it("degrades to a generic internal error for a non-JSON body", async () => {
    const err = await toApiError(new Response("<html>bad gateway</html>", { status: 502 }));
    expect([err.status, err.code]).toEqual([502, "internal"]);
  });

  it("degrades for JSON that is not an error body", async () => {
    const err = await toApiError(new Response(JSON.stringify([1]), { status: 500 }));
    expect(err.code).toBe("internal");
  });
});
