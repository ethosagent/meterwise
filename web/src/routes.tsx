import { Navigate, Route, Routes } from "react-router-dom";
import { AccessManagementPage } from "./features/access-management/Page";
import { MonitorsPage } from "./features/monitors/Page";
import { OrganizationPage } from "./features/organization/Page";
import { OverviewPage } from "./features/overview/Page";
import { ProfilePage } from "./features/profile/Page";
import { SessionsPage } from "./features/sessions/Page";
import { SetupPage } from "./features/setup/Page";
import { WorkspacePage } from "./features/workspace/Page";
import { NotFound } from "./shell/NotFound";
import { DEFAULT_PATH, pageByKey } from "./shell/nav";
import { Shell } from "./shell/Shell";

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Navigate to={DEFAULT_PATH} replace />} />
        <Route path={pageByKey("overview").path} element={<OverviewPage />} />
        <Route path={pageByKey("sessions").path} element={<SessionsPage />} />
        <Route path={pageByKey("monitors").path} element={<MonitorsPage />} />
        <Route path={pageByKey("setup").path} element={<SetupPage />} />
        <Route path="/settings" element={<Navigate to={pageByKey("profile").path} replace />} />
        <Route path={pageByKey("profile").path} element={<ProfilePage />} />
        <Route path={pageByKey("workspace").path} element={<WorkspacePage />} />
        <Route path={pageByKey("organization").path} element={<OrganizationPage />} />
        <Route path={pageByKey("access").path} element={<AccessManagementPage />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}
