/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Clerk publishable key; required for a production build to sign anyone in. */
  readonly VITE_CLERK_PUBLISHABLE_KEY?: string;
  /** Dev only: where the mock adapter mints real signed tokens (cmd/clerkmock-serve). */
  readonly VITE_MOCK_TOKEN_URL?: string;
}
