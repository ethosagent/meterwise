import { describe, expect, it } from "vitest";
import { loadAuthAdapter, MissingAuthKeyError } from "./load";

describe("loadAuthAdapter", () => {
  it("uses Clerk when a publishable key is set, even in dev", async () => {
    const adapter = await loadAuthAdapter({ publishableKey: "pk_test_x", dev: true });
    expect(adapter.kind).toBe("clerk");
  });

  it("uses the mock only in a dev server with no key", async () => {
    const adapter = await loadAuthAdapter({ publishableKey: undefined, dev: true });
    expect(adapter.kind).toBe("mock");
  });

  it("refuses a production build with no key rather than falling back to the mock", async () => {
    await expect(loadAuthAdapter({ publishableKey: undefined, dev: false })).rejects.toBeInstanceOf(
      MissingAuthKeyError,
    );
    await expect(loadAuthAdapter({ publishableKey: "", dev: false })).rejects.toThrow(
      /VITE_CLERK_PUBLISHABLE_KEY/,
    );
  });
});
