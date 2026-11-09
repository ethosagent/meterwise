import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { pageByKey } from "../../shell/nav";
import { renderAt } from "../../test/render";
import { MonitorsPage } from "./Page";

describe("MonitorsPage", () => {
  const page = pageByKey("monitors");

  it("renders the page head and an empty body at its route", () => {
    renderAt(<MonitorsPage />, page.path);
    expect(screen.getByRole("heading", { level: 2, name: page.label })).toBeInTheDocument();
    expect(screen.getByText(page.description)).toBeInTheDocument();
    expect(screen.getByText("Nothing here yet")).toBeInTheDocument();
  });

  it("is registered at /monitors", () => {
    expect(page.path).toBe("/monitors");
  });
});
