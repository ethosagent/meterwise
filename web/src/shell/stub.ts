// Placeholder identity and workspaces until Plan 02 (real auth and tenancy).
export interface StubWorkspace {
  id: string;
  name: string;
}

export const STUB_WORKSPACES: readonly StubWorkspace[] = [
  { id: "ws-default", name: "Default workspace" },
  { id: "ws-staging", name: "Staging" },
  { id: "ws-research", name: "Research" },
];

export const STUB_USER = { name: "Demo User", email: "demo@example.test" } as const;

export function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w.charAt(0).toUpperCase())
    .join("");
}
