import { Navigate, Outlet, useLocation, useSearchParams } from "react-router-dom";
import { ApiError } from "../api/errors";
import { IDENTITY_ERRORS, useMe } from "../api/identity";
import { useAuthContext } from "./AuthProvider";
import { LoadFailedScreen, LoadingScreen, NotInvitedScreen, SessionExpiredScreen } from "./screens";
import { useSession } from "./session";

/** Layout route: renders the app only for a signed-in, invited user; otherwise the right screen. */
export function AuthGate() {
  const { session } = useAuthContext();
  const expired = useSession((s) => s.expired);
  const clearExpired = useSession((s) => s.clearExpired);
  const storeSignedIn = useSession((s) => s.signedIn);
  const location = useLocation();
  const me = useMe(session.isSignedIn && storeSignedIn);
  const signOut = async () => {
    await session.signOut();
    clearExpired();
  };

  if (!session.isLoaded) return <LoadingScreen />;
  if (expired) return <SessionExpiredScreen onSignIn={signOut} />;
  if (!session.isSignedIn) return <Navigate to="/login" replace state={{ from: location }} />;
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.code === IDENTITY_ERRORS.notInvited) {
      return <NotInvitedScreen onSignOut={signOut} />;
    }
    return <LoadFailedScreen message={me.error.message} onRetry={() => me.refetch()} />;
  }
  if (!me.data) return <LoadingScreen />;
  return <Outlet />;
}

/** The sign-in route: already signed in goes to the app. */
export function LoginRoute() {
  const { adapter, session } = useAuthContext();
  if (session.isLoaded && session.isSignedIn) return <Navigate to="/" replace />;
  return <adapter.LoginScreen />;
}

/** The invitation landing. With Clerk it needs the invitation ticket; without one there is nothing to accept. */
export function AcceptInviteRoute() {
  const { adapter } = useAuthContext();
  const [params] = useSearchParams();
  if (adapter.kind === "clerk" && !params.has("__clerk_ticket"))
    return <Navigate to="/login" replace />;
  return <adapter.AcceptInviteScreen />;
}
