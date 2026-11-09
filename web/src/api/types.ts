// Local mirror of the Plan 01 API contract. The generated twin
// (src/contract/generated.ts) will supersede these once it carries the DTOs; the
// integrator then points the imports there and deletes this file.

export type ThemeChoice = "light" | "dark" | "system";

/** GET /api/config */
export interface AppConfig {
  appName: string;
  proxyPublicHost: string;
}

/** GET and PATCH /api/me/preferences */
export interface Preferences {
  theme: ThemeChoice;
  reducedMotion: boolean;
}

/** Every non-2xx JSON body. */
export interface ApiErrorBody {
  code: string;
  message: string;
  details: Record<string, unknown>;
}
