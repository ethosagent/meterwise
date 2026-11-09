import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "antd";
import { describe, expect, it } from "vitest";
import { renderAt } from "../test/render";
import { ACCESS_ERROR_TEXT } from "./access";
import { ApiError } from "./errors";
import { useErrorToast } from "./useErrorToast";

function Probe({ error, byCode }: { error: unknown; byCode?: Record<string, string> | undefined }) {
  const fail = useErrorToast(byCode);
  return (
    <button type="button" onClick={() => fail(error, "Could not do it.")}>
      fail
    </button>
  );
}

const withApp = (error: unknown, byCode?: Record<string, string>) => (
  <App>
    <Probe error={error} byCode={byCode} />
  </App>
);

describe("useErrorToast", () => {
  it("shows the readable line for a known access conflict", async () => {
    const e = new ApiError(409, { code: "last_admin", message: "x", details: {} });
    renderAt(withApp(e, ACCESS_ERROR_TEXT));
    await userEvent.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText(/without an admin/)).toBeInTheDocument();
  });

  it("without a map a coded error still falls back, and a 422 shows its first problem", async () => {
    const e = new ApiError(409, { code: "last_admin", message: "x", details: {} });
    const { unmount } = renderAt(withApp(e));
    await userEvent.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText("Could not do it.")).toBeInTheDocument();
    unmount();
    const bad = new ApiError(422, {
      code: "validation_failed",
      message: "m",
      details: { problems: ["cidrs: bad"] },
    });
    renderAt(withApp(bad));
    await userEvent.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText("cidrs: bad")).toBeInTheDocument();
  });

  it("falls back to the caller's wording for anything else", async () => {
    renderAt(withApp(new Error("boom")));
    await userEvent.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText("Could not do it.")).toBeInTheDocument();
  });
});
