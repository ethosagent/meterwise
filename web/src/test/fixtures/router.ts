import type { RoutePair, RouterSettings, RouterStats } from "../../contract/generated";

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

/** Seven days of routing: certain savings, one refused target, and a guardrail that is fine. */
export const ROUTER_STATS: RouterStats = {
  range: "7d",
  from: "2026-11-05",
  to: "2026-11-11",
  mode: "conservative",
  calls: 40,
  routedCalls: 10,
  observedCalls: 0,
  unavailable: 1,
  worseFeedback: 1,
  routedBp: 2500,
  savedUsd: "0.022400",
  wouldSaveUsd: "0.000000",
  guardrail: {
    cheapCalls: 9,
    strongCalls: 30,
    cheapFireBp: 500,
    strongFireBp: 400,
    cheapCompletedBp: 9000,
    strongCompletedBp: 9200,
    warn: false,
  },
  byPair: [
    { provider: "openai", from: "gpt-4.1", to: "gpt-4.1-mini", calls: 10, savedUsd: "0.022400" },
  ],
};
