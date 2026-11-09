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

function useMockSession(): SessionApi {
  const email = useMockAuth((s) => s.email);
  const signOut = useMockAuth((s) => s.signOut);
  const getToken = useCallback(async () => (email ? mockToken(email) : null), [email]);
  return useMemo(
    () => ({
      isLoaded: true,
      isSignedIn: email !== null,
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
