import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { AuditEntry } from "../../api/contract04";
import type { PermissionSnapshot } from "../../contract/generated";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { AccessManagementPage } from "./Page";

const PATH = "/settings/access";
const OPTION = { selector: ".ant-select-item-option-content" };

function entry(n: number, over: Partial<AuditEntry> = {}): AuditEntry {
  return {
    id: `aud_${n}`,
    at: new Date(Date.UTC(2026, 10, 9, 12, 0, 0) - n * 60_000).toISOString(),
    actor: { userId: "u1", email: "ada@acme.test", name: "Ada Admin" },
    action: "workspace.update",
    resourceType: "workspace",
    resourceId: "ws_1",
    workspaceId: "ws_1",
    ip: "203.0.113.9",
    metadata: {},
    ...over,
  };
}

async function openAudit() {
  await userEvent.click(await screen.findByRole("tab", { name: "Audit log" }));
}

describe("Audit log tab", () => {
  it("lists When, Actor, Action, Resource and IP, newest first; a missing actor or IP reads plainly", async () => {
    mockApi({
      audit: [
        entry(1),
        entry(2, { actor: null, ip: null, action: "workspace.url_reset", resourceId: "ws_2" }),
      ],
    });
    renderAt(<AccessManagementPage />, PATH);
    await openAudit();
    for (const h of ["When", "Actor", "Action", "Resource", "IP"]) {
      expect(await screen.findByRole("columnheader", { name: h })).toBeInTheDocument();
    }
    const rows = await screen.findAllByRole("row");
    const first = within(rows[1] as HTMLElement);
    expect(first.getByText("Ada Admin")).toBeInTheDocument();
    expect(first.getByText("workspace.update")).toBeInTheDocument();
    expect(first.getByText("203.0.113.9")).toBeInTheDocument();
    const second = within(rows[2] as HTMLElement);
    expect(second.getByText("System")).toBeInTheDocument();
    expect(second.getByText("—")).toBeInTheDocument();
    expect(second.getByText("Staging")).toBeInTheDocument();
  });

  it("pages with Load more by the cursor, appending to the table", async () => {
    const api = mockApi({ audit: Array.from({ length: 60 }, (_, i) => entry(i + 1)) });
    renderAt(<AccessManagementPage />, PATH);
    await openAudit();
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(51)); // header + 50
    await userEvent.click(screen.getByRole("button", { name: "Load more" }));
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(61));
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    const calls = api.callsTo("GET", /\/api\/audit-log/);
    expect(calls[0]?.path).not.toContain("cursor");
    expect(calls[1]?.path).toContain("cursor=50");
  });

  it("filters by action and by workspace through the query string", async () => {
    const api = mockApi({
      audit: [entry(1), entry(2, { action: "workspace.url_reset", workspaceId: "ws_2" })],
    });
    renderAt(<AccessManagementPage />, PATH);
    await openAudit();
    await userEvent.click(await screen.findByRole("combobox", { name: "Filter by action" }));
    await userEvent.click(await screen.findByText("workspace.url_reset", OPTION));
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(2));
    expect(api.callsTo("GET", /action=workspace\.url_reset/)).not.toHaveLength(0);
    await userEvent.click(screen.getByRole("combobox", { name: "Filter by workspace" }));
    await userEvent.click(await screen.findByText("Staging", OPTION));
    await waitFor(() => expect(api.callsTo("GET", /workspaceId=ws_2/)).not.toHaveLength(0));
    // clearing a filter drops it from the next request
    const before = api.callsTo("GET", /\/api\/audit-log/).length;
    const clear = screen
      .getByRole("combobox", { name: "Filter by action" })
      .closest(".ant-select")
      ?.querySelector(".ant-select-clear");
    await userEvent.click(clear as Element);
    await waitFor(() =>
      expect(api.callsTo("GET", /\/api\/audit-log/).length).toBeGreaterThan(before),
    );
    expect(api.callsTo("GET", /\/api\/audit-log/).at(-1)?.path).not.toContain("action=");
  });

  it("needs audit_log:read: a member reader without it gets no tab, one with only it gets just the tab", async () => {
    const readers: PermissionSnapshot = {
      admin: false,
      grants: [
        ...MEMBER_SNAPSHOT.grants,
        { workspaceId: null, resourceType: "member", action: "read" },
      ],
    };
    mockApi({ me: MEMBER_ME, access: { snapshot: readers } });
    const a = renderAt(<AccessManagementPage />, PATH);
    await screen.findByRole("tab", { name: "Groups" });
    expect(screen.queryByRole("tab", { name: "Audit log" })).toBeNull();
    a.unmount();
    const onlyAudit: PermissionSnapshot = {
      admin: false,
      grants: [{ workspaceId: null, resourceType: "audit_log", action: "read" }],
    };
    mockApi({ me: MEMBER_ME, access: { snapshot: onlyAudit } });
    renderAt(<AccessManagementPage />, PATH);
    expect(await screen.findByRole("tab", { name: "Audit log" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Groups" })).toBeNull();
    expect(screen.queryByText("You do not have access to this page")).toBeNull();
  });

  it("an empty log says so", async () => {
    mockApi({
      access: {
        snapshot: {
          admin: false,
          grants: [{ workspaceId: null, resourceType: "audit_log", action: "read" }],
        },
      },
      me: MEMBER_ME,
    });
    renderAt(<AccessManagementPage />, PATH);
    await openAudit();
    expect(await screen.findByText("No audit entries.")).toBeInTheDocument();
  });
});
