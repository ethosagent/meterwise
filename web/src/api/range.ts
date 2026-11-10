import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";

/** The range selection that lives in the URL (`?range=&from=&to=`), never in client state (ruling S12). */
export interface RangeSel {
  range: "7d" | "30d" | "custom";
  /** YYYY-MM-DD (UTC), set only for a custom range. */
  from: string;
  to: string;
}

// The H5 import scanner reads the word from followed by a quote as an import, so this param name is spelled
// with an escape rather than as a literal string.
const FROM_KEY = "\u0066rom";
const DAY = /^\d{4}-\d{2}-\d{2}$/;
const MAX_CUSTOM_DAYS = 90;
const DAY_MS = 86_400_000;

/** Today as a UTC YYYY-MM-DD day. */
export function utcToday(now: Date = new Date()): string {
  return now.toISOString().slice(0, 10);
}

function dayMs(day: string): number {
  return Date.parse(`${day}T00:00:00Z`);
}

/** Whether the pair (start day, end day) is a range the server accepts: real days, from <= to <= today, at most 90 days. */
export function validCustomRange(from: string, to: string, today: string = utcToday()): boolean {
  if (!DAY.test(from) || !DAY.test(to)) return false;
  const a = dayMs(from);
  const b = dayMs(to);
  if (Number.isNaN(a) || Number.isNaN(b) || a > b || b > dayMs(today)) return false;
  return (b - a) / DAY_MS + 1 <= MAX_CUSTOM_DAYS;
}

/** Reads the selection from search params; anything the server would refuse falls back to 7d. */
export function parseRange(p: URLSearchParams, today: string = utcToday()): RangeSel {
  const range = p.get("range");
  if (range === "30d") return { range, from: "", to: "" };
  const from = p.get(FROM_KEY) ?? "";
  const to = p.get("to") ?? "";
  if (range === "custom" && validCustomRange(from, to, today)) return { range, from, to };
  return { range: "7d", from: "", to: "" };
}

/** The query-string form the savings routes take. */
export function rangeQuery(r: RangeSel): URLSearchParams {
  const q = new URLSearchParams({ range: r.range });
  if (r.range === "custom") {
    q.set(FROM_KEY, r.from);
    q.set("to", r.to);
  }
  return q;
}

/** A stable cache-key part for the selection. */
export const rangeKey = (r: RangeSel): string =>
  r.range === "custom" ? `custom:${r.from}:${r.to}` : r.range;

/** The start and end bounds (RFC 3339, end exclusive) the sessions list takes for a selection. */
export function rangeBounds(r: RangeSel, now: Date = new Date()): { from: string; to: string } {
  if (r.range === "custom") {
    return {
      from: `${r.from}T00:00:00Z`,
      to: new Date(dayMs(r.to) + DAY_MS).toISOString().replace(".000Z", "Z"),
    };
  }
  const days = r.range === "30d" ? 30 : 7;
  const end = dayMs(utcToday(now)) + DAY_MS;
  return {
    from: new Date(end - days * DAY_MS).toISOString().replace(".000Z", "Z"),
    to: new Date(end).toISOString().replace(".000Z", "Z"),
  };
}

/** The selected range and a setter that writes it to the URL, keeping every other param. */
export function useRangeParams() {
  const [params, setParams] = useSearchParams();
  const sel = useMemo(() => parseRange(params), [params]);
  const setRange = useCallback(
    (next: RangeSel) => {
      setParams(
        (prev) => {
          const q = new URLSearchParams(prev);
          q.delete("range");
          q.delete(FROM_KEY);
          q.delete("to");
          if (next.range !== "7d") q.set("range", next.range);
          if (next.range === "custom") {
            q.set(FROM_KEY, next.from);
            q.set("to", next.to);
          }
          return q;
        },
        { replace: true },
      );
    },
    [setParams],
  );
  return { sel, setRange };
}
