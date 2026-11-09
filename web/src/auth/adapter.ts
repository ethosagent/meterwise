import type { ComponentType, ReactNode } from "react";

/** What the app needs to know about the signed-in session, whichever provider backs it. */
export interface SessionApi {
  isLoaded: boolean;
  isSignedIn: boolean;
  /** A fresh bearer token for the API, or null when signed out. */
  getToken: () => Promise<string | null>;
  signOut: () => Promise<void>;
}

/** The seam between the app and an identity provider (Clerk in production, a mock in tests and dev). */
export interface AuthAdapter {
  kind: "clerk" | "mock";
  /** Set by providers that need configuration (Clerk's publishable key); handed to `Provider`. */
  publishableKey?: string;
  Provider: ComponentType<{ children: ReactNode; publishableKey?: string | undefined }>;
  /** Called inside `Provider`. */
  useAuthSession: () => SessionApi;
  /** Sign-in, including the provider's own forgot and reset steps. */
  LoginScreen: ComponentType;
  /** The invitation landing: finish creating the account from the emailed link. */
  AcceptInviteScreen: ComponentType;
}
