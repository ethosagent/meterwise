const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
];

/** "2 minutes ago", "just now": a past instant relative to `now` (injectable for tests). */
export function relativeTime(iso: string, now: Date = new Date()): string {
  const seconds = Math.round((now.getTime() - new Date(iso).getTime()) / 1000);
  if (seconds < 60) return "just now";
  const fmt = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  for (const [unit, size] of UNITS) {
    if (seconds >= size) return fmt.format(-Math.floor(seconds / size), unit);
  }
  return "just now";
}
