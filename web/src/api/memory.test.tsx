import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useForgetMemory, useMemoryHitFeedback } from "./memory";

const WS = "w1";

function setup(response: Response) {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(response);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const spy = vi.spyOn(qc, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return { spy, wrapper };
}

const invalidated = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map((c) => (c[0] as { queryKey: unknown[] }).queryKey.slice(1).join("/"));

afterEach(() => vi.restoreAllMocks());

describe("memory mutations invalidate only what they change", () => {
  it("a hint feedback refreshes the session, the stats and the trace details, never the traces pages or settings", async () => {
    const { spy, wrapper } = setup(new Response("{}", { status: 200, headers: { "content-type": "application/json" } }));
    const { result } = renderHook(() => useMemoryHitFeedback(WS, "s1"), { wrapper });
    result.current.mutate({ hitId: "9", feedback: "helpful" });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const keys = invalidated(spy);
    expect(keys).toContain("memory/w1/stats");
    expect(keys).toContain("memory/w1/trace");
    expect(keys).not.toContain("memory/w1");
    expect(keys).not.toContain("memory/w1/traces");
  });

  it("forget refreshes stats, the traces list and details, not the memory settings", async () => {
    const { spy, wrapper } = setup(new Response(null, { status: 204 }));
    const { result } = renderHook(() => useForgetMemory(WS), { wrapper });
    result.current.mutate();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const keys = invalidated(spy);
    expect(keys).toEqual(expect.arrayContaining(["memory/w1/stats", "memory/w1/traces", "memory/w1/trace", "savings/w1"]));
    expect(keys).not.toContain("memory/w1");
    expect(keys).not.toContain("memory/w1/config");
  });
});
