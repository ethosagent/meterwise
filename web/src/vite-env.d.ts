/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Clerk publishable key; required for a production build to sign anyone in. */
  readonly VITE_CLERK_PUBLISHABLE_KEY?: string;
}
