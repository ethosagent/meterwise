import { Divider } from "antd";
import type { SessionDetail } from "../../contract/generated";
import { MemoryHintEvent } from "../memory/MemoryHintEvent";
import { RouteEvent } from "../router/RouteEvent";
import { TrimEvent } from "../trim/TrimEvent";
import { EvidenceCard } from "./EvidenceCard";
import type { StepEvent } from "./stepEvents";

/** One event of a step, drawn by its kind. A monitor fire keeps its evidence card; the savers get a one-line card. */
export function EventCard({
  event,
  detail,
  workspaceId,
  highlighted,
}: {
  event: StepEvent;
  detail: SessionDetail;
  workspaceId: string;
  highlighted: boolean;
}) {
  switch (event.kind) {
    case "monitor_fire":
      return (
        <EvidenceCard
          event={event.event}
          calls={detail.calls}
          workspaceId={workspaceId}
          sessionId={detail.session.id}
          highlighted={highlighted}
        />
      );
    case "trim":
      return (
        <>
          <Divider size="small" />
          <TrimEvent call={event.call} workspaceId={workspaceId} />
        </>
      );
    case "route":
      return (
        <>
          <Divider size="small" />
          <RouteEvent call={event.call} workspaceId={workspaceId} sessionId={detail.session.id} />
        </>
      );
    case "memory_hint":
      return (
        <>
          <Divider size="small" />
          <MemoryHintEvent
            hit={event.hit}
            workspaceId={workspaceId}
            sessionId={detail.session.id}
          />
        </>
      );
  }
}
