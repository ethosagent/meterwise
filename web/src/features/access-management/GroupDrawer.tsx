import {
  Button,
  Drawer,
  Input,
  Popconfirm,
  Select,
  Skeleton,
  Tabs,
  Tag,
  Typography,
  theme,
} from "antd";
import { useState } from "react";
import {
  ACCESS_ERROR_TEXT,
  useAttachWorkspace,
  useDeleteGroup,
  useDetachWorkspace,
  useGroupGrants,
  usePutGroupGrants,
  useRenameGroup,
} from "../../api/access";
import { useWorkspaces } from "../../api/identity";
import { useErrorToast } from "../../api/useErrorToast";
import type { Group } from "../../contract/generated";
import { GrantsEditor } from "./GrantsEditor";
import { MembersTransfer } from "./MembersTransfer";

interface Props {
  group: Group | undefined;
  onClose: () => void;
  /** PermAdmin: every write in the drawer. Without it the drawer is read-only. */
  isAdmin: boolean;
}

function Details({ group, onClose, isAdmin }: Props & { group: Group }) {
  const { token } = theme.useToken();
  const workspaces = useWorkspaces().data ?? [];
  const rename = useRenameGroup();
  const del = useDeleteGroup();
  const attach = useAttachWorkspace();
  const detach = useDetachWorkspace();
  const [name, setName] = useState(group.name);
  const fail = useErrorToast(ACCESS_ERROR_TEXT);
  const nameOf = (id: string) => workspaces.find((w) => w.id === id)?.name ?? id;
  return (
    <>
      <Typography.Title level={5}>Name</Typography.Title>
      <Input.Search
        aria-label="Group name"
        value={name}
        maxLength={80}
        disabled={!isAdmin}
        enterButton="Rename"
        loading={rename.isPending}
        onChange={(e) => setName(e.target.value)}
        onSearch={(v) =>
          rename.mutate(
            { id: group.id, name: v.trim() },
            { onError: (e) => fail(e, "Could not rename the group.") },
          )
        }
      />
      <Typography.Title level={5} style={{ marginTop: token.marginLG }}>
        Workspaces
      </Typography.Title>
      <div style={{ marginBottom: token.marginXS }}>
        {group.workspaceIds.length === 0 ? (
          <Typography.Text type="secondary">None attached.</Typography.Text>
        ) : null}
        {group.workspaceIds.map((id) => (
          <Tag
            key={id}
            closable={isAdmin}
            onClose={(e) => {
              e.preventDefault();
              detach.mutate(
                { id: group.id, workspaceId: id },
                { onError: (err) => fail(err, "Could not detach the workspace.") },
              );
            }}
          >
            {nameOf(id)}
          </Tag>
        ))}
      </div>
      <Select<string | null>
        aria-label="Attach a workspace"
        placeholder="Attach a workspace"
        disabled={!isAdmin}
        value={null}
        style={{ minWidth: token.controlHeightLG * 5 }}
        options={workspaces
          .filter((w) => !group.workspaceIds.includes(w.id))
          .map((w) => ({ value: w.id, label: w.name }))}
        onChange={(workspaceId) =>
          workspaceId !== null &&
          attach.mutate(
            { id: group.id, workspaceId },
            { onError: (e) => fail(e, "Could not attach the workspace.") },
          )
        }
      />
      <Typography.Paragraph type="secondary" style={{ marginTop: token.marginXS }}>
        Attaching grants read on the workspace; detaching removes every grant this group has there.
      </Typography.Paragraph>
      <Tabs
        items={[
          {
            key: "permissions",
            label: "Permissions",
            children: <Permissions group={group} isAdmin={isAdmin} nameOf={nameOf} />,
          },
          {
            key: "members",
            label: "Members",
            children: <MembersTransfer groupId={group.id} disabled={!isAdmin} />,
          },
        ]}
      />
      {isAdmin ? (
        <Popconfirm
          title={`Delete ${group.name}?`}
          description="Members keep their organization access but lose what this group granted."
          okText="Delete"
          okButtonProps={{ danger: true }}
          onConfirm={() =>
            del.mutate(group.id, {
              onSuccess: onClose,
              onError: (e) => fail(e, "Could not delete the group."),
            })
          }
        >
          <Button danger loading={del.isPending}>
            Delete group
          </Button>
        </Popconfirm>
      ) : null}
    </>
  );
}

function Permissions({
  group,
  isAdmin,
  nameOf,
}: {
  group: Group;
  isAdmin: boolean;
  nameOf: (id: string) => string;
}) {
  const grants = useGroupGrants(group.id);
  const put = usePutGroupGrants();
  if (!grants.data) return <Skeleton active />;
  const sections = [
    ...group.workspaceIds.map((id) => ({ title: nameOf(id), workspaceId: id })),
    { title: "Organization", workspaceId: null },
  ];
  // Remount the editor whenever the server's copy changes, so a refetch replaces a stale draft.
  const key = JSON.stringify([group.workspaceIds, grants.data]);
  return (
    <>
      {group.workspaceIds.length === 0 ? (
        <Typography.Paragraph type="secondary">
          Attach a workspace to give this group access to it.
        </Typography.Paragraph>
      ) : null}
      <GrantsEditor
        key={key}
        sections={sections}
        saved={grants.data}
        disabled={!isAdmin}
        saving={put.isPending}
        save={(next, on) => put.mutate({ id: group.id, grants: next }, on)}
      />
    </>
  );
}

/** Group detail: name, attached workspaces, the Permissions matrix and the Members transfer list. */
export function GroupDrawer(props: Props) {
  return (
    <Drawer
      open={props.group !== undefined}
      onClose={props.onClose}
      size="large"
      title={props.group?.name ?? "Group"}
      destroyOnHidden
    >
      {props.group ? <Details {...props} group={props.group} /> : null}
    </Drawer>
  );
}
