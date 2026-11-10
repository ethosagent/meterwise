import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LoadMore } from "./LoadMore";

describe("LoadMore", () => {
  it("renders nothing without a next page", () => {
    render(<LoadMore hasMore={false} loading={false} onMore={() => undefined} />);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("asks for the next page on click", async () => {
    const onMore = vi.fn();
    render(<LoadMore hasMore loading={false} onMore={onMore} />);
    await userEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(onMore).toHaveBeenCalledTimes(1);
  });
});
