import type { Call, MonitorEvent, SessionDetail } from "../../contract/generated";

/** The call a deep link points at: `?event=<eventId>` (its call) or `?call=<callId>`. Never a call index, which restarts. */
export function highlightedCallId(
  detail: SessionDetail,
  eventId: string | null,
  callId: string | null,
): string | null {
  if (eventId) {
    const ev = detail.monitorEvents.find((e) => e.id === eventId);
    if (ev) return ev.callId;
  }
  if (callId && detail.calls.some((c) => c.id === callId)) return callId;
  return null;
}

/** The monitor events of one call, in the order the server sent them. */
export function eventsOf(detail: SessionDetail, callId: string): MonitorEvent[] {
  return detail.monitorEvents.filter((e) => e.callId === callId);
}

/** An evidence step index as something to show: a link target when it is a call of this session, else "earlier call". */
export interface StepRef {
  label: string;
  callId: string | null;
}

/**
 * Evidence points at earlier calls by index. An index of 0 or below (after a tollgate idle reset or
 * a restart the ring starts over) or one this session no longer holds is "earlier call" and is not linked.
 */
export function stepRef(index: number, calls: readonly Call[]): StepRef {
  if (index <= 0) return { label: "earlier call", callId: null };
  const hit = calls.find((c) => c.callIndex === index);
  return hit
    ? { label: `call #${index}`, callId: hit.id }
    : { label: "earlier call", callId: null };
}

/** The certain block of a blocked call, when its event was returned (the detail caps events). */
export function blockEventOf(detail: SessionDetail, callId: string): MonitorEvent | undefined {
  return eventsOf(detail, callId).find((e) => e.action === "block");
}

/** "12m 30s" between two instants; "0s" when they coincide or are reversed. */
export function duration(startIso: string, endIso: string): string {
  const total = Math.max(0, Math.round((Date.parse(endIso) - Date.parse(startIso)) / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}
