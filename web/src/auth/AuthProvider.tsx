import { createContext, type ReactNode, useContext, useLayoutEffect } from "react";
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
  const { isSignedIn, getToken } = session;
  useLayoutEffect(() => setSignedIn(isSignedIn, getToken), [isSignedIn, getToken, setSignedIn]);
  return <AuthContext.Provider value={{ adapter, session }}>{children}</AuthContext.Provider>;
}

/** Hosts the identity provider and publishes the session. Everything signed-in sits below it. */
export function AuthProvider({ adapter, children }: { adapter: AuthAdapter; children: ReactNode }) {
  return (
    <adapter.Provider>
      <Bridge adapter={adapter}>{children}</Bridge>
    </adapter.Provider>
  );
}

export function useAuthContext(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuthContext needs an AuthProvider above it");
  return ctx;
}
