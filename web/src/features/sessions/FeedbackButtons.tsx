import { DislikeFilled, DislikeOutlined, LikeFilled, LikeOutlined } from "@ant-design/icons";
import { Button, Space } from "antd";
import { type Feedback, useFeedback } from "../../api/sessions";
import { useErrorToast } from "../../api/useErrorToast";

/**
 * 👍/👎 on one monitor event. Clicking the pressed one clears it (null). The stored value is what
 * shows: a successful POST refetches the session, so a failed one leaves the old state in place.
 */
export function FeedbackButtons({
  workspaceId,
  sessionId,
  eventId,
  value,
}: {
  workspaceId: string;
  sessionId: string;
  eventId: string;
  value: string | null;
}) {
  const mutation = useFeedback(workspaceId, sessionId);
  const toast = useErrorToast();
  const send = (next: Feedback) =>
    mutation.mutate(
      { eventId, feedback: next },
      { onError: (e) => toast(e, "Could not save your feedback.") },
    );
  const correct = value === "correct";
  const wrong = value === "wrong";
  return (
    <Space size="small" role="group" aria-label="Was this right?">
      <Button
        size="small"
        aria-label="Mark correct"
        aria-pressed={correct}
        disabled={mutation.isPending}
        icon={correct ? <LikeFilled /> : <LikeOutlined />}
        onClick={() => send(correct ? null : "correct")}
      />
      <Button
        size="small"
        aria-label="Mark wrong"
        aria-pressed={wrong}
        disabled={mutation.isPending}
        icon={wrong ? <DislikeFilled /> : <DislikeOutlined />}
        onClick={() => send(wrong ? null : "wrong")}
      />
    </Space>
  );
}
