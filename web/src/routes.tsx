import type { ReactElement } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { useActiveWorkspace } from "./api/identity";
import { AcceptInviteRoute, AuthGate, LoginRoute } from "./auth/AuthGate";
import { AccessManagementPage } from "./features/access-management/Page";
import { MonitorsPage } from "./features/monitors/Page";
import { OrganizationPage } from "./features/organization/Page";
import { OverviewPage } from "./features/overview/Page";
import { ProfilePage } from "./features/profile/Page";
import { SessionsPage } from "./features/sessions/Page";
import { SessionDetailPage } from "./features/sessions/SessionDetailPage";
import { SetupPage } from "./features/setup/Page";
import { WorkspacePage } from "./features/workspace/Page";
import { NotFound } from "./shell/NotFound";
import { NoWorkspaceAccess } from "./shell/NoWorkspaceAccess";
import { ALL_PAGES, DEFAULT_PATH, type NavKey, pageByKey } from "./shell/nav";
import { Shell } from "./shell/Shell";

/** One element per nav key. The Record type makes a page without a route (or a route without a page) a compile error. */
const PAGE_ELEMENTS: Record<NavKey, ReactElement> = {
  overview: <OverviewPage />,
  sessions: <SessionsPage />,
  monitors: <MonitorsPage />,
  setup: <SetupPage />,
  profile: <ProfilePage />,
  workspace: <WorkspacePage />,
  organization: <OrganizationPage />,
  access: <AccessManagementPage />,
};

/** Pages about the active workspace; the rest (profile, organization, access) work without one. */
const WORKSPACE_PAGES: readonly NavKey[] = [
  "overview",
  "sessions",
  "monitors",
  "setup",
  "workspace",
];

/** With no readable workspace (Me.activeWorkspaceId is null) a workspace page is an explanation, not an error. */
function WorkspaceGate({ children }: { children: ReactElement }) {
  return useActiveWorkspace().state === "none" ? <NoWorkspaceAccess /> : children;
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login/*" element={<LoginRoute />} />
      <Route path="/accept-invite/*" element={<AcceptInviteRoute />} />
      <Route element={<AuthGate />}>
        <Route element={<Shell />}>
          <Route index element={<Navigate to={DEFAULT_PATH} replace />} />
          <Route path="/settings" element={<Navigate to={pageByKey("profile").path} replace />} />
          {ALL_PAGES.map((p) => (
            <Route
              key={p.key}
              path={p.path}
              element={
                WORKSPACE_PAGES.includes(p.key) ? (
                  <WorkspaceGate>{PAGE_ELEMENTS[p.key]}</WorkspaceGate>
                ) : (
                  PAGE_ELEMENTS[p.key]
                )
              }
            />
          ))}
          <Route
            path="/sessions/:sessionId"
            element={
              <WorkspaceGate>
                <SessionDetailPage />
              </WorkspaceGate>
            }
          />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Route>
    </Routes>
  );
}
