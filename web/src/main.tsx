import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { loadAuthAdapter } from "./auth/load";
import "./index.css";

const root = document.getElementById("root");
if (!root) throw new Error("#root is missing from index.html");
const mount = createRoot(root);

// A build with no publishable key shows an error screen: it never falls back to the mock identity provider.
loadAuthAdapter()
  .then((adapter) => ({ adapter }))
  .catch((e: unknown) => ({ error: e instanceof Error ? e.message : "Sign-in failed to load." }))
  .then((state) =>
    mount.render(
      <StrictMode>
        <App auth={"adapter" in state ? state.adapter : state} />
      </StrictMode>,
    ),
  );
