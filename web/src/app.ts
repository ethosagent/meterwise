import { ERROR_STATUS, type ErrorCode, ROUTES, type RouteSpec } from "./contract/generated";

export const PRODUCT_NAME = "Meterwise";

/** Public routes the shell may call before login (from the generated route table). */
export function publicRoutes(routes: readonly RouteSpec[] = ROUTES): RouteSpec[] {
  return routes.filter((r) => r.public);
}

/** Human-facing summary of an API error code; unknown codes degrade to a generic line. */
export function describeError(code: string): string {
  if (!(code in ERROR_STATUS)) return "Something went wrong.";
  const status = ERROR_STATUS[code as ErrorCode];
  return status >= 500 ? "The server hit a problem. Try again." : "The request was not accepted.";
}

/** Mounts the shell. Plan 01 replaces this with the React application. */
export function mount(root: HTMLElement): void {
  root.textContent = PRODUCT_NAME;
}
