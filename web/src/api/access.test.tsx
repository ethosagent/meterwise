import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { BO, type MockGroup } from "../test/mockAccess";
import { mockApi } from "../test/mockApi";
import { renderAt } from "../test/render";
import { useGroupMembership, useGroups } from "./access";

const GROUP: MockGroup = {
  id: "g1",
  name: "Engineers",
  memberCount: 0,
  workspaceIds: [],
  createdAt: "2026-11-01T00:00:00Z",
  grants: [],
  memberIds: [],
};

function Probe() {
  const groups = useGroups(true);
  const move = useGroupMembership();
  return (
    <>
      <output>{groups.data?.[0]?.memberCount ?? "?"}</output>
      <button
        type="button"
        onClick={() => move.mutate({ id: "g1", add: [BO.userId, "ghost"], remove: [] })}
      >
        go
      </button>
    </>
  );
}

describe("useGroupMembership", () => {
  it("refreshes the lists even when a later request in the sequence fails (half applied)", async () => {
    const api = mockApi({ access: { groups: [GROUP] } });
    renderAt(<Probe />);
    await screen.findByText("0");
    await userEvent.click(screen.getByRole("button", { name: "go" }));
    // the first add applied, the second (not a member) was refused with 422
    await waitFor(() => expect(api.callsTo("POST", "/api/groups/g1/members")).toHaveLength(2));
    expect(await screen.findByText("1")).toBeInTheDocument();
  });
});
