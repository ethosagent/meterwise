import { useQueryClient } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useLayoutEffect, useState } from "react";
import { resetForbiddenRefresh } from "../api/queryClient";
import type { AuthAdapter, SessionApi } from "./adapter";
import { useSession } from "./session";

interface AuthContextValue {
  adapter: AuthAdapter;
  session: SessionApi;
}

const AuthContext = createContext<AuthContextValue | null>(null);

/** Mirrors the adapter's session into the store the fetch layer reads, then exposes it to the tree. */
function Bridge({ adapter, children }: { adapter: AuthAdapter; children: ReactNode }) {
  const session = adapter.useAuthSession();
  const setSignedIn = useSession((s) => s.setSignedIn);
  const { isSignedIn, getToken, userId } = session;
  useLayoutEffect(() => setSignedIn(isSignedIn, getToken), [isSignedIn, getToken, setSignedIn]);

  // Everything cached belongs to one identity (Me, the permission snapshot and workspaces never go
  // stale by themselves). When the identity changes, drop the whole cache, and render nothing
  // below until it is dropped, so the new person never sees one frame of the previous one's data.
  const qc = useQueryClient();
  const [settledFor, setSettledFor] = useState(userId);
  useLayoutEffect(() => {
    if (settledFor === userId) return;
    qc.clear();
    resetForbiddenRefresh(qc);
    setSettledFor(userId);
  }, [settledFor, userId, qc]);
  if (settledFor !== userId) return null;
  return <AuthContext.Provider value={{ adapter, session }}>{children}</AuthContext.Provider>;
}

/** Hosts the identity provider and publishes the session. Everything signed-in sits below it. */
export function AuthProvider({ adapter, children }: { adapter: AuthAdapter; children: ReactNode }) {
  return (
    <adapter.Provider publishableKey={adapter.publishableKey}>
      <Bridge adapter={adapter}>{children}</Bridge>
    </adapter.Provider>
  );
}

export function useAuthContext(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuthContext needs an AuthProvider above it");
  return ctx;
}
