import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "antd";
import { describe, expect, it, vi } from "vitest";
import { renderAt } from "../test/render";
import { useCopy } from "./useCopy";

function Probe({ text, label }: { text: string; label: string }) {
  const copy = useCopy(text, label);
  return (
    <button type="button" onClick={copy}>
      copy
    </button>
  );
}

const view = (text: string, label: string) => (
  <App>
    <Probe text={text} label={label} />
  </App>
);

describe("useCopy", () => {
  it("writes the text to the clipboard and says what was copied", async () => {
    const user = userEvent.setup();
    renderAt(view("fixture-text", "Proxy URL"));
    await user.click(screen.getByRole("button", { name: "copy" }));
    expect(await navigator.clipboard.readText()).toBe("fixture-text");
    expect(await screen.findByText("Proxy URL copied")).toBeInTheDocument();
  });

  it("tells the person to copy by hand when the clipboard refuses", async () => {
    const user = userEvent.setup();
    renderAt(view("x", "Proxy URL"));
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(new Error("denied"));
    await user.click(screen.getByRole("button", { name: "copy" }));
    expect(await screen.findByText(/Could not copy/)).toBeInTheDocument();
  });
});
