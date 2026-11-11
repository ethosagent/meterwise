import { monitorLabel } from "../monitors/monitorMeta";

/** The three savers beyond the monitors; the cause names are the contract's SAVINGS_CAUSES. */
const SAVER_LABEL: Record<string, string> = {
  trim: "Trimming",
  router: "Model routing",
  memory: "Memory",
};

/** A readable name for a savings cause: a saver's own name, else the monitor's. */
export function causeLabel(cause: string): string {
  return SAVER_LABEL[cause] ?? monitorLabel(cause);
}

const MODE_WORD: Record<string, string> = {
  off: "Off",
  observe: "Observe",
  note: "Nudge",
  block: "Block",
  lossless: "Lossless",
  aggressive: "Aggressive",
  conservative: "Conservative",
  hint: "Hint",
};

/** A readable mode: monitors keep their Nudge wording; an unknown mode shows as sent. */
export function modeLabel(mode: string): string {
  return MODE_WORD[mode] ?? mode;
}
