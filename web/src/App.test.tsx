import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";
import { mockApi } from "./test/mockApi";

describe("App", () => {
  it("shows a configuration error, not a mock sign-in, when auth could not be set up", async () => {
    mockApi();
    render(<App auth={{ error: "VITE_CLERK_PUBLISHABLE_KEY is not set" }} />);
    expect(await screen.findByText("Sign-in is not configured")).toBeInTheDocument();
    expect(screen.getByText(/VITE_CLERK_PUBLISHABLE_KEY/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Email")).toBeNull();
  });
});
