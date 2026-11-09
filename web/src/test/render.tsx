import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ComponentType, ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { expect } from "vitest";
import { createQueryClient } from "../api/queryClient";
import { AuthProvider } from "../auth/AuthProvider";
import { mockAdapter, useMockAuth } from "../auth/mock";
import { useSession } from "../auth/session";
import { type NavKey, pageByKey } from "../shell/nav";
import { ThemeProvider } from "../theme/ThemeProvider";
import { mockApi } from "./mockApi";

export function testQueryClient(): QueryClient {
  return createQueryClient(false);
}

interface RenderOptions {
  /** Signed in with the mock identity provider (default), or signed out. */
  signedInAs?: string | null;
  client?: QueryClient;
}

/**
 * The one provider stack for tests: query client, theme, router, mock auth.
 * Components and the whole app render inside it exactly as in production.
 */
export function renderAt(ui: ReactElement, path = "/", opts: RenderOptions = {}) {
  const client = opts.client ?? testQueryClient();
  useSession.setState({ expired: false });
  const email = opts.signedInAs === undefined ? "ada@acme.test" : opts.signedInAs;
  if (email) useMockAuth.getState().signIn(email);
  else useMockAuth.getState().signOut();
  const result = render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[path]}>
          <AuthProvider adapter={mockAdapter}>{ui}</AuthProvider>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return { ...result, client };
}

/** A page that has not been built yet renders its head and an empty body at its own route. */
export function expectStubPage(Component: ComponentType, key: NavKey) {
  const page = pageByKey(key);
  mockApi();
  renderAt(<Component />, page.path);
  expect(screen.getByRole("heading", { level: 2, name: page.label })).toBeInTheDocument();
  expect(screen.getByText(page.description)).toBeInTheDocument();
  expect(screen.getByText("Nothing here yet")).toBeInTheDocument();
}
