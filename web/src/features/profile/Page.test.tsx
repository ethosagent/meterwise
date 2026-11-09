import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { pageByKey } from "../../shell/nav";
import { renderAt } from "../../test/render";
import { ProfilePage } from "./Page";

describe("ProfilePage", () => {
  const page = pageByKey("profile");

  it("renders the page head and an empty body at its route", () => {
    renderAt(<ProfilePage />, page.path);
    expect(screen.getByRole("heading", { level: 2, name: page.label })).toBeInTheDocument();
    expect(screen.getByText(page.description)).toBeInTheDocument();
    expect(screen.getByText("Nothing here yet")).toBeInTheDocument();
  });

  it("is registered at /settings/profile", () => {
    expect(page.path).toBe("/settings/profile");
  });
});
