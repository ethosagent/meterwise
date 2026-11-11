import type { Call, MemoryHitView, MonitorEvent, SessionDetail } from "../../contract/generated";
import { formatTokens } from "../../lib/money";
import { eventsOf } from "./stepModel";

/** One thing that happened at a step. The kinds follow the hook order: trim, memory, router, then the monitors. */
export type StepEvent =
  | { kind: "trim"; key: string; call: Call }
  | { kind: "memory_hint"; key: string; hit: MemoryHitView }
  | { kind: "route"; key: string; call: Call }
  | { kind: "monitor_fire"; key: string; event: MonitorEvent };

/** The savers' and monitors' events of one call, in hook order. */
export function stepEvents(detail: SessionDetail, call: Call): StepEvent[] {
  const out: StepEvent[] = [];
  if (call.trimAction !== null) out.push({ kind: "trim", key: `trim-${call.id}`, call });
  for (const hit of detail.memoryHits) {
    if (hit.callId === call.id) out.push({ kind: "memory_hint", key: `hit-${hit.id}`, hit });
  }
  if (call.routerAction !== null) out.push({ kind: "route", key: `route-${call.id}`, call });
  for (const event of eventsOf(detail, call.id)) {
    out.push({ kind: "monitor_fire", key: event.id, event });
  }
  return out;
}

/** The small tags a step header shows for the savers that touched it. */
export function saverTags(call: Call, hasHit: boolean): string[] {
  const tags: string[] = [];
  if (call.trimAction === "acted") tags.push(`Trimmed −${formatTokens(call.trimTokens)} tokens`);
  if (call.routerAction === "routed" && call.routedTo) tags.push(`Routed → ${call.routedTo}`);
  if (hasHit && call.hintInjected) tags.push("Remembered");
  return tags;
}
