import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { BO, type MockGroup } from "../test/mockAccess";
import { mockApi } from "../test/mockApi";
import { renderAt } from "../test/render";
import {
  useAttachWorkspace,
  useCreateGroup,
  useDeleteGroup,
  useDetachWorkspace,
  useGroupMembership,
  useGroups,
  usePermissionSnapshot,
} from "./access";

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

function RefreshProbe() {
  usePermissionSnapshot();
  const create = useCreateGroup();
  const del = useDeleteGroup();
  return (
    <>
      <p>{create.status}</p>
      <p>del:{del.status}</p>
      <button type="button" onClick={() => create.mutate("Ops")}>
        create
      </button>
      <button type="button" onClick={() => del.mutate("g1")}>
        delete
      </button>
    </>
  );
}

describe("what a mutation refreshes", () => {
  it("creating a group leaves the caller's permissions alone; deleting one refetches them", async () => {
    const api = mockApi({ access: { groups: [GROUP] } });
    renderAt(<RefreshProbe />);
    await waitFor(() => expect(api.callsTo("GET", "/api/me/permissions")).toHaveLength(1));
    await userEvent.click(screen.getByRole("button", { name: "create" }));
    await screen.findByText("success");
    expect(api.callsTo("GET", "/api/me/permissions")).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "delete" }));
    await screen.findByText("del:success");
    expect(api.callsTo("GET", "/api/me/permissions")).toHaveLength(2);
  });
});

function WsProbe() {
  const attach = useAttachWorkspace();
  const detach = useDetachWorkspace();
  return (
    <>
      <button type="button" onClick={() => attach.mutate({ id: "g1", workspaceId: "ws_2" })}>
        attach
      </button>
      <button type="button" onClick={() => detach.mutate({ id: "g1", workspaceId: "ws_2" })}>
        detach
      </button>
    </>
  );
}

describe("attach and detach", () => {
  it("attach POSTs the workspace and detach DELETEs it", async () => {
    const api = mockApi({ access: { groups: [GROUP] } });
    renderAt(<WsProbe />);
    await userEvent.click(await screen.findByRole("button", { name: "attach" }));
    await waitFor(() => expect(api.callsTo("POST", "/api/groups/g1/workspaces")).toHaveLength(1));
    expect(api.callsTo("POST", "/api/groups/g1/workspaces")[0]?.body).toEqual({
      workspaceId: "ws_2",
    });
    await userEvent.click(screen.getByRole("button", { name: "detach" }));
    await waitFor(() =>
      expect(api.callsTo("DELETE", "/api/groups/g1/workspaces/ws_2")).toHaveLength(1),
    );
  });
});
