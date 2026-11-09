import { beforeEach, describe, expect, it } from "vitest";
import { useSession } from "./session";

const token = async () => "t";

beforeEach(() => useSession.setState({ signedIn: false, expired: false }));

describe("session store", () => {
  it("clears a stale expired flag when a sign-in arrives by any path", () => {
    useSession.getState().markExpired();
    useSession.getState().setSignedIn(true, token);
    expect(useSession.getState().expired).toBe(false);
    expect(useSession.getState().signedIn).toBe(true);
  });

  it("keeps the expired flag when the update is a sign-out", () => {
    useSession.getState().markExpired();
    useSession.getState().setSignedIn(false, token);
    expect(useSession.getState().expired).toBe(true);
  });
});
