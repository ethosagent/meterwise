import { Alert, App, Button, Select, Space, Typography } from "antd";
import { useState } from "react";
import { describeApiError } from "../../api/errors";
import { ALLOWED_CIDRS_MAX } from "../../api/limits";
import { useSetAllowedCidrs } from "../../api/setup";

/**
 * Allowed client CIDRs: a tags input. Empty means any client may use the URL; set, other source IPs get 403.
 * The server validates and normalises each entry (a bare IP becomes /32 or /128). Shown with workspace_url:manage.
 */
export function AllowedCidrs({ workspaceId, saved }: { workspaceId: string; saved: string[] }) {
  const { message } = App.useApp();
  const save = useSetAllowedCidrs();
  const [draft, setDraft] = useState<string[] | null>(null);
  const value = draft ?? saved;
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(saved);
  return (
    <Space vertical style={{ width: "100%" }}>
      <Typography.Text strong id="allowed-cidrs-label">
        Allowed client CIDRs
      </Typography.Text>
      <Typography.Text type="secondary">
        When set, requests from any other source IP are refused with 403. Leave empty to allow any
        client.
      </Typography.Text>
      {save.isError ? (
        <Alert
          type="error"
          showIcon
          title={describeApiError(save.error, { fallback: "Could not save the allowed CIDRs." })}
        />
      ) : null}
      <Select
        mode="tags"
        aria-labelledby="allowed-cidrs-label"
        style={{ width: "100%" }}
        tokenSeparators={[",", " "]}
        open={false}
        placeholder="203.0.113.0/24, 2001:db8::/32"
        value={value}
        onChange={(v: string[]) => setDraft(v.map((x) => x.trim()).filter(Boolean))}
      />
      <Typography.Text type="secondary">
        {value.length} of {ALLOWED_CIDRS_MAX}
      </Typography.Text>
      <Button
        type="primary"
        disabled={!dirty}
        loading={save.isPending}
        onClick={() =>
          save.mutate(
            { workspaceId, cidrs: value },
            {
              onSuccess: () => {
                setDraft(null);
                message.success("Allowed CIDRs saved");
              },
            },
          )
        }
      >
        Save CIDRs
      </Button>
    </Space>
  );
}
