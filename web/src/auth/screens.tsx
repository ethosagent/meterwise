import { Button, Result, Spin } from "antd";
import { useAppName } from "../shell/Brand";
import { AuthLayout } from "./AuthLayout";

export function LoadingScreen() {
  return (
    <AuthLayout>
      <Spin size="large" aria-label="Loading" />
    </AuthLayout>
  );
}

/** 403 not_invited: signed in with the identity provider, but no invitation and no membership. */
export function NotInvitedScreen({ onSignOut }: { onSignOut: () => void }) {
  const appName = useAppName();
  return (
    <AuthLayout>
      <Result
        status="warning"
        title="You have not been invited yet"
        subTitle={`${appName ?? "This workspace"} is invite-only. Ask an administrator to send you an invitation, then sign in with the email address it was sent to.`}
        extra={<Button onClick={onSignOut}>Sign out</Button>}
      />
    </AuthLayout>
  );
}

/** 401 on an authenticated call: the session ended while the app was open. */
export function SessionExpiredScreen({ onSignIn }: { onSignIn: () => void }) {
  return (
    <AuthLayout>
      <Result
        status="info"
        title="Your session expired"
        subTitle="Sign in again to continue. Nothing you saved was lost."
        extra={
          <Button type="primary" onClick={onSignIn}>
            Sign in again
          </Button>
        }
      />
    </AuthLayout>
  );
}

export function LoadFailedScreen({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <AuthLayout>
      <Result
        status="error"
        title="Could not load your account"
        subTitle={message}
        extra={<Button onClick={onRetry}>Try again</Button>}
      />
    </AuthLayout>
  );
}

/** A production build with no publishable key: refuse, rather than fall back to a mock. */
export function AuthMisconfiguredScreen({ message }: { message: string }) {
  return (
    <AuthLayout>
      <Result status="error" title="Sign-in is not configured" subTitle={message} />
    </AuthLayout>
  );
}
