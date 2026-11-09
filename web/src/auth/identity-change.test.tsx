import { act, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { usePermissionSnapshot } from "../api/access";
import { MEMBER_SNAPSHOT } from "../test/mockAccess";
import { MEMBER_ME, mockApi } from "../test/mockApi";
import { renderAt } from "../test/render";
import { useMockAuth } from "./mock";

const seen: Array<boolean | undefined> = [];

function Probe() {
  const admin = usePermissionSnapshot().data?.admin;
  seen.push(admin);
  return <output data-testid="admin">{String(admin)}</output>;
}

describe("a new identity on the same tab", () => {
  it("never renders the previous user's permission snapshot (A signs out, B signs in)", async () => {
    seen.length = 0;
    const api = mockApi({ access: { snapshot: { admin: true, grants: [] } } });
    renderAt(<Probe />, "/", { signedInAs: "ada@acme.test" });
    await screen.findByText("true");

    const mark = seen.length;
    act(() => useMockAuth.getState().signOut());
    api.access.setSnapshot(MEMBER_SNAPSHOT); // the server now answers as B, a plain member
    act(() => useMockAuth.getState().signIn("bo@acme.test"));

    await screen.findByText("false");
    expect(seen.slice(mark)).not.toContain(true);
    expect(MEMBER_ME.role).toBe("member");
  });

  it("also clears on a direct switch with no signed-out render in between", async () => {
    seen.length = 0;
    const api = mockApi({ access: { snapshot: { admin: true, grants: [] } } });
    renderAt(<Probe />, "/", { signedInAs: "ada@acme.test" });
    await screen.findByText("true");
    const mark = seen.length;
    api.access.setSnapshot(MEMBER_SNAPSHOT);
    act(() => useMockAuth.getState().signIn("bo@acme.test"));
    await screen.findByText("false");
    expect(seen.slice(mark)).not.toContain(true);
  });
});
