import { afterEach, describe, expect, it, vi } from "vitest";
import { mintToken, mockToken } from "./mock";

afterEach(() => vi.unstubAllGlobals());

describe("mintToken (dev mock adapter)", () => {
  it("is the plain mock string when no mint URL is configured", async () => {
    expect(await mintToken("ada@acme.test", undefined)).toBe(mockToken("ada@acme.test"));
  });

  it("asks the Clerk twin for a signed token for the email", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ token: "a.b.c" })));
    vi.stubGlobal("fetch", fetchMock);
    expect(await mintToken("ada+x@acme.test", "http://127.0.0.1:9444/mint")).toBe("a.b.c");
    expect(fetchMock).toHaveBeenCalledWith("http://127.0.0.1:9444/mint?email=ada%2Bx%40acme.test");
  });

  it("fails loudly when the mint refuses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("no", { status: 500 })),
    );
    await expect(mintToken("ada@acme.test", "http://127.0.0.1:9444/mint")).rejects.toThrow(/500/);
  });
});
