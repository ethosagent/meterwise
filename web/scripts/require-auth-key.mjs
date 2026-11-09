// Release builds must carry the Clerk publishable key: without it the bundle cannot sign anyone in,
// and the app refuses to fall back to the mock identity provider. `npm run build` (the CI gate) stays keyless;
// the image build and `make web` use `npm run build:release`.
import { loadEnv } from "vite";

const key = loadEnv("production", process.cwd(), "VITE_").VITE_CLERK_PUBLISHABLE_KEY;
if (!key) {
  console.error(
    "build:release needs VITE_CLERK_PUBLISHABLE_KEY (env or web/.env.production.local).",
  );
  process.exit(1);
}
