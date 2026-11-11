import type { MemorySettings } from "../../contract/generated";

/** The default memory settings: off, and not enabled for want of a key (no memory model is configured). */
export const MEMORY_SETTINGS: MemorySettings = {
  config: { mode: "off", thresholdBp: 8200, maxHintsPerSession: 1, model: "", embedModel: "" },
  enabled: false,
  disabledReason: "no_key",
};
