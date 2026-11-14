import type { MemorySettings, MemoryStats } from "../../contract/generated";

/** Why memory is not running, as the Memory row of the Savers card says it. */
export const DISABLED_NOTE: Record<string, string> = {
  no_key: "Not enabled: no memory model key is configured on this server.",
  privacy: "privacy mode: off until capture is enabled",
  budget: "Paused: today's memory budget is used",
};

/** The same reasons as a page-level explanation (Overview card, traces browser). */
export const DISABLED_PAGE: Record<string, string> = {
  no_key: "Memory is not configured on this server",
  privacy: "Memory needs capture on for this workspace; privacy mode keeps it off",
  budget: "Paused: today's memory budget is used",
};

/**
 * What memory is doing, in one sentence, in the order the customer meets it: not enabled (and why), off, nothing
 * learned yet, learning and observing, then hinting. `null` while the figures are loading.
 */
export function memoryStateText(
  settings: MemorySettings,
  stats: MemoryStats | undefined,
): string | null {
  const reason = settings.disabledReason;
  if (!settings.enabled && reason) return DISABLED_PAGE[reason] ?? reason;
  if (settings.config.mode === "off") return "Memory is off. Turn on Observe to start learning.";
  if (!stats) return null;
  if (stats.traces === 0) return "Memory starts learning after your first completed session";
  const learned = `${stats.traces} ${stats.traces === 1 ? "trace" : "traces"} learned`;
  return settings.config.mode === "observe" ? `${learned}, observing` : learned;
}
