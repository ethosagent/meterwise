import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "antd";
import { describe, expect, it } from "vitest";
import { renderAt } from "../test/render";
import { ApiError } from "./errors";
import { useAccessToast } from "./useAccessToast";

function Probe({ error }: { error: unknown }) {
  const fail = useAccessToast();
  return (
    <button type="button" onClick={() => fail(error, "Could not do it.")}>
      fail
    </button>
  );
}

const withApp = (error: unknown) => (
  <App>
    <Probe error={error} />
  </App>
);

describe("useAccessToast", () => {
  it("shows the readable line for a known access conflict", async () => {
    const e = new ApiError(409, { code: "last_admin", message: "x", details: {} });
    renderAt(withApp(e));
    await userEvent.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText(/without an admin/)).toBeInTheDocument();
  });

  it("falls back to the caller's wording for anything else", async () => {
    renderAt(withApp(new Error("boom")));
    await userEvent.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText("Could not do it.")).toBeInTheDocument();
  });
});
