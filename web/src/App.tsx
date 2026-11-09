import { QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { BrowserRouter } from "react-router-dom";
import { createQueryClient } from "./api/queryClient";
import { AuthProvider } from "./auth/AuthProvider";
import type { AuthAdapter } from "./auth/adapter";
import { AuthMisconfiguredScreen } from "./auth/screens";
import { AppRoutes } from "./routes";
import { ThemeProvider } from "./theme/ThemeProvider";

/** Root: server-state client, theme, router, identity provider. `auth` is the adapter, or why there is none. */
export function App({ auth }: { auth: AuthAdapter | { error: string } }) {
  const [client] = useState(() => createQueryClient(1));
  return (
    <QueryClientProvider client={client}>
      <ThemeProvider>
        {"error" in auth ? (
          <AuthMisconfiguredScreen message={auth.error} />
        ) : (
          <BrowserRouter>
            <AuthProvider adapter={auth}>
              <AppRoutes />
            </AuthProvider>
          </BrowserRouter>
        )}
      </ThemeProvider>
    </QueryClientProvider>
  );
}
