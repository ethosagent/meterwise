import type { AuthAdapter } from "./adapter";

/** Thrown when a production build has no Clerk key. There is deliberately no fallback to the mock. */
export class MissingAuthKeyError extends Error {
  constructor() {
    super("VITE_CLERK_PUBLISHABLE_KEY is not set: this build cannot sign anyone in.");
    this.name = "MissingAuthKeyError";
  }
}

interface Env {
  publishableKey: string | undefined;
  dev: boolean;
}

/**
 * Picks the identity provider: Clerk when a key is set, the mock only in a dev server, otherwise an error.
 * The mock sits behind `import.meta.env.DEV`, so a production bundle does not contain it.
 */
export async function loadAuthAdapter(
  env: Env = {
    publishableKey: import.meta.env.VITE_CLERK_PUBLISHABLE_KEY,
    dev: import.meta.env.DEV,
  },
): Promise<AuthAdapter> {
  if (env.publishableKey) {
    const { clerkAdapter } = await import("./clerk");
    return clerkAdapter(env.publishableKey);
  }
  if (env.dev && import.meta.env.DEV) {
    const { mockAdapter } = await import("./mock");
    return mockAdapter;
  }
  throw new MissingAuthKeyError();
}
