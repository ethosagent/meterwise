import type { Role } from "../contract/generated";

/** The two roles, in display order; one list for the invite form and the members table. */
export const ROLE_OPTIONS: { value: Role; label: string }[] = [
  { value: "member", label: "Member" },
  { value: "admin", label: "Admin" },
];
