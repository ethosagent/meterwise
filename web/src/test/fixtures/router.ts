import type { RoutePair, RouterSettings } from "../../contract/generated";

export const DEFAULT_PAIRS: RoutePair[] = [
  { provider: "openai", from: "gpt-4.1", to: "gpt-4.1-mini" },
  { provider: "anthropic", from: "claude-sonnet-4-5", to: "claude-haiku-4-5" },
];

/** The default router settings: off, no workspace map, the priced defaults in effect. */
export const ROUTER_SETTINGS: RouterSettings = {
  config: { mode: "off", thresholdBp: null, map: null },
  defaults: DEFAULT_PAIRS,
  suggestions: [],
  effective: DEFAULT_PAIRS,
};
