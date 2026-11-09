import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { ResetUrl } from "./ResetUrl";

describe("ResetUrl", () => {
  it("says the URL was reset but cannot be shown when the server returns proxyUrl null", async () => {
    const manageOnly = {
      admin: false,
      grants: [
        ...MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "workspace_url"),
        { workspaceId: null, resourceType: "workspace_url", action: "manage" },
      ],
    };
    mockApi({ me: MEMBER_ME, access: { snapshot: manageOnly } });
    renderAt(<ResetUrl workspaceId="ws_1" />, "/settings/workspace");
    await userEvent.click(await screen.findByRole("button", { name: "Reset URL" }));
    const pop = within(document.querySelector(".ant-popover") as HTMLElement);
    await userEvent.click(await pop.findByRole("button", { name: "Reset URL" }));
    expect(
      await screen.findByText(/URL reset; you don't have permission to view it/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/\/w\//)).toBeNull();
  });
});
