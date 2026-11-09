import { Alert, App, Button, InputNumber, Popconfirm, Space, Typography } from "antd";
import { useState } from "react";
import { useResetUrl, useRetirePrevious } from "../../api/setup";
import { useErrorToast } from "../../api/useErrorToast";
import {
  GRACE_MINUTES_DEFAULT,
  GRACE_MINUTES_MAX,
  type ResetURLResponse,
} from "../../contract/generated";

/**
 * Reset URL: a Popconfirm that explains the grace window, then a notice that the previous URL
 * still works until it ends, with a button to end it now. The caller shows this only with workspace_url:manage.
 */
export function ResetUrl({ workspaceId }: { workspaceId: string }) {
  const { message } = App.useApp();
  const reset = useResetUrl();
  const retire = useRetirePrevious();
  const [grace, setGrace] = useState<number>(GRACE_MINUTES_DEFAULT);
  const [result, setResult] = useState<ResetURLResponse | null>(null);
  const fail = useErrorToast();
  return (
    <Space vertical style={{ width: "100%" }}>
      <Popconfirm
        title="Reset this workspace's URL?"
        description={
          <Space vertical>
            <Typography.Text>
              A new URL is issued. The current one keeps working for the grace period so you can
              update your agents, then it stops with a 410.
            </Typography.Text>
            <Space>
              <label htmlFor="grace-minutes">Grace period (minutes)</label>
              <InputNumber
                id="grace-minutes"
                min={0}
                max={GRACE_MINUTES_MAX}
                precision={0}
                value={grace}
                onChange={(v) => setGrace(v ?? GRACE_MINUTES_DEFAULT)}
              />
            </Space>
          </Space>
        }
        okText="Reset URL"
        okButtonProps={{ danger: true }}
        onConfirm={() =>
          reset.mutate(
            { workspaceId, graceMinutes: grace },
            {
              onSuccess: (r) => {
                setResult(r);
                message.success("URL reset");
              },
              onError: (e) => fail(e, "Could not reset the URL."),
            },
          )
        }
      >
        <Button danger loading={reset.isPending}>
          Reset URL
        </Button>
      </Popconfirm>
      {result ? (
        <Alert
          type="info"
          showIcon
          title={
            result.proxyUrl === null
              ? `URL reset; you don't have permission to view it. The previous URL still works until ${new Date(result.previousGraceUntil).toLocaleTimeString()}.`
              : `The previous URL still works until ${new Date(result.previousGraceUntil).toLocaleTimeString()}.`
          }
          action={
            <Button
              size="small"
              loading={retire.isPending}
              onClick={() =>
                retire.mutate(workspaceId, {
                  onSuccess: () => setResult(null),
                  onError: (e) => fail(e, "Could not end the grace period."),
                })
              }
            >
              End grace period now
            </Button>
          }
        />
      ) : null}
    </Space>
  );
}
