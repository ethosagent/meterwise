import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { useRangeParams, utcToday } from "../api/range";
import { renderAt } from "../test/render";
import { RangeControl } from "./RangeControl";

function Harness({ custom = true }: { custom?: boolean }) {
  const { sel, setRange } = useRangeParams();
  const { search } = useLocation();
  return (
    <>
      <RangeControl value={sel} onChange={setRange} custom={custom} />
      <output aria-label="url">{search}</output>
    </>
  );
}
const url = () => screen.getByLabelText("url").textContent;

describe("RangeControl", () => {
  it("writes 30d to the URL and drops the param for the 7d default, keeping other params", async () => {
    renderAt(<Harness />, "/x?client=curl");
    await userEvent.click(screen.getByText("30 days"));
    expect(url()).toBe("?client=curl&range=30d");
    await userEvent.click(screen.getByText("7 days"));
    expect(url()).toBe("?client=curl");
  });

  it("applies a custom range only once both days are valid", async () => {
    renderAt(<Harness />, "/x");
    await userEvent.click(screen.getByText("Custom"));
    expect(url()).toBe("");
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-11-01" } });
    expect(url()).toBe("");
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-11-05" } });
    expect(url()).toBe("?range=custom&from=2026-11-01&to=2026-11-05");
  });

  it("refuses a span over 90 days or a day after today, and says why", async () => {
    renderAt(<Harness />, "/x");
    await userEvent.click(screen.getByText("Custom"));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2020-01-01" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: utcToday() } });
    expect(url()).toBe("");
    expect(screen.getByText(/Pick up to 90 UTC days/)).toBeInTheDocument();
  });

  it("without custom offers only 7 and 30 days", () => {
    renderAt(<Harness custom={false} />, "/x");
    expect(screen.queryByText("Custom")).toBeNull();
    expect(screen.getByText("30 days")).toBeInTheDocument();
  });
});
