import { App, Button, Flex, Typography, theme } from "antd";
import { useState } from "react";
import type { Grant } from "../../access/plan03-types";
import { ACCESS_ERROR_TEXT, usePutGroupGrants } from "../../api/access";
import { describeApiError } from "../../api/errors";
import { type MatrixSection, PermissionsMatrix } from "./PermissionsMatrix";

const norm = (gs: Grant[]) =>
  gs
    .map((g) => `${g.workspaceId ?? ""}|${g.resourceType}|${g.action}`)
    .sort()
    .join(",");

interface Props {
  groupId: string;
  sections: MatrixSection[];
  saved: Grant[];
  disabled: boolean;
}

/** The draft of a group's grants. The parent remounts it (key) when the server's copy changes. */
export function GrantsEditor({ groupId, sections, saved, disabled }: Props) {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const put = usePutGroupGrants();
  const [draft, setDraft] = useState<Grant[]>(saved);
  const dirty = norm(draft) !== norm(saved);
  return (
    <>
      {sections.length === 1 ? (
        <Typography.Paragraph type="secondary">
          Attach a workspace to give this group access to it.
        </Typography.Paragraph>
      ) : null}
      <PermissionsMatrix
        sections={sections}
        grants={draft}
        onChange={setDraft}
        disabled={disabled}
      />
      <Flex gap={token.marginSM}>
        <Button
          type="primary"
          disabled={disabled || !dirty}
          loading={put.isPending}
          onClick={() =>
            put.mutate(
              { id: groupId, grants: draft },
              {
                onSuccess: () => message.success("Permissions saved"),
                onError: (e) =>
                  message.error(
                    describeApiError(e, {
                      byCode: ACCESS_ERROR_TEXT,
                      fallback: "Could not save the permissions.",
                    }),
                  ),
              },
            )
          }
        >
          Save permissions
        </Button>
        <Button disabled={!dirty} onClick={() => setDraft(saved)}>
          Discard changes
        </Button>
      </Flex>
    </>
  );
}
