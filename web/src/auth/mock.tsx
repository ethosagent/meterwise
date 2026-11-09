import { Button, Form, Input, Typography } from "antd";
import { type ReactNode, useCallback, useMemo } from "react";
import { create } from "zustand";
import { AuthCard, AuthLayout } from "./AuthLayout";
import type { AuthAdapter, SessionApi } from "./adapter";

const KEY = "auth.mock.email";

function readEmail(): string | null {
  try {
    return sessionStorage.getItem(KEY);
  } catch {
    return null;
  }
}

function writeEmail(email: string | null) {
  try {
    if (email) sessionStorage.setItem(KEY, email);
    else sessionStorage.removeItem(KEY);
  } catch {
    // memory only
  }
}

interface MockAuthState {
  email: string | null;
  signIn: (email: string) => void;
  signOut: () => void;
}

/** State behind the mock identity provider. Tests and local dev only: never bundled in production. */
export const useMockAuth = create<MockAuthState>((set) => ({
  email: readEmail(),
  signIn: (email) => {
    writeEmail(email);
    set({ email });
  },
  signOut: () => {
    writeEmail(null);
    set({ email: null });
  },
}));

/** The mock "token" is just the email: the dev mock API reads it back. */
export const mockToken = (email: string) => `mock:${email}`;

/**
 * Dev only. With VITE_MOCK_TOKEN_URL set (the end-to-end run), the token is a real signed JWT minted
 * by the Clerk twin (cmd/clerkmock-serve) for the signed-in email, fetched fresh on every call so
 * the control plane verifies a genuine signature. Without it, the plain `mock:<email>` string.
 */
export async function mintToken(email: string, mintUrl = import.meta.env.VITE_MOCK_TOKEN_URL) {
  if (!mintUrl) return mockToken(email);
  const resp = await fetch(`${mintUrl}?email=${encodeURIComponent(email)}`);
  if (!resp.ok) throw new Error(`token mint failed: ${resp.status}`);
  return ((await resp.json()) as { token: string }).token;
}

function useMockSession(): SessionApi {
  const email = useMockAuth((s) => s.email);
  const signOut = useMockAuth((s) => s.signOut);
  const getToken = useCallback(async () => (email ? mintToken(email) : null), [email]);
  return useMemo(
    () => ({
      isLoaded: true,
      isSignedIn: email !== null,
      userId: email,
      getToken,
      signOut: async () => signOut(),
    }),
    [email, getToken, signOut],
  );
}

function MockForm({ action }: { action: string }) {
  const signIn = useMockAuth((s) => s.signIn);
  return (
    <AuthLayout>
      <AuthCard>
        <Typography.Paragraph type="secondary">
          Development sign-in (mock identity provider).
        </Typography.Paragraph>
        <Form
          layout="vertical"
          onFinish={(v: { email: string }) => signIn(v.email.trim())}
          requiredMark={false}
        >
          <Form.Item
            label="Email"
            name="email"
            rules={[{ required: true, type: "email", message: "Enter a valid email" }]}
          >
            <Input autoComplete="email" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>
            {action}
          </Button>
        </Form>
      </AuthCard>
    </AuthLayout>
  );
}

export const mockAdapter: AuthAdapter = {
  kind: "mock",
  Provider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useAuthSession: useMockSession,
  LoginScreen: () => <MockForm action="Sign in" />,
  AcceptInviteScreen: () => <MockForm action="Accept invitation" />,
};
