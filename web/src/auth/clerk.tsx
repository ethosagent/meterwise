import { ClerkProvider, SignIn, SignUp, useAuth } from "@clerk/react";
import { theme } from "antd";
import { type ReactNode, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { AuthLayout } from "./AuthLayout";
import type { AuthAdapter, SessionApi } from "./adapter";

export const LOGIN_PATH = "/login";
export const ACCEPT_INVITE_PATH = "/accept-invite";

/** Clerk's component theme, taken from the active antd tokens so light and dark both match. */
function useClerkAppearance() {
  const { token } = theme.useToken();
  return useMemo(
    () => ({
      variables: {
        colorPrimary: token.colorPrimary,
        colorBackground: token.colorBgElevated,
        colorText: token.colorText,
        colorTextSecondary: token.colorTextSecondary,
        colorInputBackground: token.colorBgContainer,
        colorInputText: token.colorText,
        colorDanger: token.colorError,
        colorSuccess: token.colorSuccess,
        fontFamily: token.fontFamily,
        borderRadius: `${token.borderRadius}px`,
      },
    }),
    [token],
  );
}

function useClerkSession(): SessionApi {
  const { isLoaded, isSignedIn, userId, getToken, signOut } = useAuth();
  return useMemo(
    () => ({
      isLoaded,
      isSignedIn: isSignedIn === true,
      userId: isSignedIn ? (userId ?? null) : null,
      getToken: async () => (isSignedIn ? await getToken() : null),
      signOut: async () => signOut(),
    }),
    [isLoaded, isSignedIn, userId, getToken, signOut],
  );
}

function LoginScreen() {
  const appearance = useClerkAppearance();
  // Clerk's SignIn also hosts forgot-password and reset-password. There is no sign-up link:
  // the Clerk instance has sign-ups disabled and people join by invitation only.
  return (
    <AuthLayout>
      <SignIn routing="path" path={LOGIN_PATH} appearance={appearance} />
    </AuthLayout>
  );
}

function AcceptInviteScreen() {
  const appearance = useClerkAppearance();
  // Reached only with the invitation ticket in the URL; the ticket is what lets Clerk create the account.
  return (
    <AuthLayout>
      <SignUp
        routing="path"
        path={ACCEPT_INVITE_PATH}
        signInUrl={LOGIN_PATH}
        appearance={appearance}
      />
    </AuthLayout>
  );
}

function Provider({
  children,
  publishableKey,
}: {
  children: ReactNode;
  publishableKey?: string | undefined;
}) {
  const navigate = useNavigate();
  if (!publishableKey) throw new Error("the Clerk adapter was built without a publishable key");
  return (
    <ClerkProvider
      publishableKey={publishableKey}
      signInUrl={LOGIN_PATH}
      afterSignOutUrl={LOGIN_PATH}
      routerPush={(to) => navigate(to)}
      routerReplace={(to) => navigate(to, { replace: true })}
    >
      {children}
    </ClerkProvider>
  );
}

/** Clerk-backed adapter for one publishable key. The key travels on the adapter, not in module state. */
export function clerkAdapter(publishableKey: string): AuthAdapter {
  return {
    kind: "clerk",
    publishableKey,
    Provider,
    useAuthSession: useClerkSession,
    LoginScreen,
    AcceptInviteScreen,
  };
}
