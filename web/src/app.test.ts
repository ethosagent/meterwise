import { describe, expect, it } from "vitest";
import { describeError, mount, PRODUCT_NAME, publicRoutes } from "./app";

describe("app shell", () => {
  it("lists only public routes", () => {
    const routes = publicRoutes([
      { method: "GET", pattern: "/a", permission: "", public: true },
      { method: "GET", pattern: "/b", permission: "x:read", public: false },
    ]);
    expect(routes.map((r) => r.pattern)).toEqual(["/a"]);
  });

  it("uses the generated route table by default", () => {
    expect(publicRoutes().some((r) => r.pattern === "/healthz")).toBe(true);
  });

  it("describes known and unknown error codes", () => {
    expect(describeError("internal")).toMatch(/server/);
    expect(describeError("forbidden")).toMatch(/not accepted/);
    expect(describeError("made_up")).toBe("Something went wrong.");
  });

  it("mounts the product name", () => {
    const el = { textContent: "" } as HTMLElement;
    mount(el);
    expect(el.textContent).toBe(PRODUCT_NAME);
  });
});
