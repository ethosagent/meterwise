import {
  ApartmentOutlined,
  AppstoreOutlined,
  DashboardOutlined,
  DeploymentUnitOutlined,
  HistoryOutlined,
  LockOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { ReactNode } from "react";
import type { PermissionName as Permission } from "../access/taxonomy";

export type NavKey =
  | "overview"
  | "sessions"
  | "monitors"
  | "setup"
  | "profile"
  | "workspace"
  | "organization"
  | "access";

export interface NavPage {
  key: NavKey;
  path: string;
  label: string;
  description: string;
  icon: ReactNode;
  /** Parent group label for the breadcrumb, when the page lives under a group. */
  group?: string;
  /** An org-scoped permission needed to see the destination at all; the gateway would 403 the page's data without it. */
  permission?: Permission;
}

export const SETTINGS_LABEL = "Settings";

/** Top-level destinations, in sidebar order. */
export const MAIN_PAGES: readonly NavPage[] = [
  {
    key: "overview",
    path: "/overview",
    label: "Overview",
    description: "Tokens and money saved across the workspace.",
    icon: <DashboardOutlined />,
  },
  {
    key: "sessions",
    path: "/sessions",
    label: "Sessions",
    description: "Every agent session and where its tokens went.",
    icon: <HistoryOutlined />,
  },
  {
    key: "monitors",
    path: "/monitors",
    label: "Monitors",
    description: "Waste detectors watching your traffic.",
    icon: <ThunderboltOutlined />,
  },
  {
    key: "setup",
    path: "/setup",
    label: "Setup",
    description: "Connect an agent to this workspace.",
    icon: <DeploymentUnitOutlined />,
  },
];

/** Pages under the Settings group, in sidebar order. */
export const SETTINGS_PAGES: readonly NavPage[] = [
  {
    key: "profile",
    path: "/settings/profile",
    label: "Profile",
    description: "Your name, sign-in and personal preferences.",
    icon: <UserOutlined />,
    group: SETTINGS_LABEL,
  },
  {
    key: "workspace",
    path: "/settings/workspace",
    label: "Workspace",
    description: "This workspace's name, limits and privacy mode.",
    icon: <AppstoreOutlined />,
    group: SETTINGS_LABEL,
  },
  {
    key: "organization",
    path: "/settings/organization",
    label: "Organization",
    description: "The organization that owns your workspaces.",
    icon: <ApartmentOutlined />,
    group: SETTINGS_LABEL,
  },
  {
    key: "access",
    path: "/settings/access",
    label: "Access management",
    description: "Who can see and change what.",
    icon: <LockOutlined />,
    group: SETTINGS_LABEL,
    permission: "member:read",
  },
];

export const ALL_PAGES: readonly NavPage[] = [...MAIN_PAGES, ...SETTINGS_PAGES];
export const DEFAULT_PATH = MAIN_PAGES[0]?.path ?? "/";
export const SETTINGS_ICON = <SettingOutlined />;

export function pageByKey(key: NavKey): NavPage {
  const page = ALL_PAGES.find((p) => p.key === key);
  if (!page) throw new Error(`unknown nav key ${key}`);
  return page;
}

export function pageByPath(pathname: string): NavPage | undefined {
  return ALL_PAGES.find((p) => p.path === pathname);
}
