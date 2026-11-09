import { App, Button, Flex, theme } from "antd";
import { useState } from "react";
import { ACCESS_ERROR_TEXT } from "../../api/access";
import { describeApiError } from "../../api/errors";
import type { Grant } from "../../contract/generated";
import { type MatrixSection, PermissionsMatrix } from "./PermissionsMatrix";

const norm = (gs: Grant[]) =>
  gs
    .map((g) => `${g.workspaceId ?? ""}|${g.resourceType}|${g.action}`)
    .sort()
    .join(",");

interface Props {
  sections: MatrixSection[];
  saved: Grant[];
  disabled: boolean;
  /** Replace all grants (one PUT); the hook's mutate and pending flag. */
  save: (grants: Grant[], on: { onSuccess: () => void; onError: (e: unknown) => void }) => void;
  saving: boolean;
}

/** The draft of a set of grants. The parent remounts it (key) when the server's copy changes. */
export function GrantsEditor({ sections, saved, disabled, save, saving }: Props) {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const [draft, setDraft] = useState<Grant[]>(saved);
  const dirty = norm(draft) !== norm(saved);
  return (
    <>
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
          loading={saving}
          onClick={() =>
            save(draft, {
              onSuccess: () => message.success("Permissions saved"),
              onError: (e) =>
                message.error(
                  describeApiError(e, {
                    byCode: ACCESS_ERROR_TEXT,
                    fallback: "Could not save the permissions.",
                  }),
                ),
            })
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
