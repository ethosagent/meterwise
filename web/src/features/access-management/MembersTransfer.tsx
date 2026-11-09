import { App, Skeleton, Transfer } from "antd";
import {
  ACCESS_ERROR_TEXT,
  useGroupMembers,
  useGroupMembership,
  useMembers,
} from "../../api/access";
import { describeApiError } from "../../api/errors";

/** Two lists: organization members on the left, this group's members on the right. Moving touches group_users only. */
export function MembersTransfer({ groupId, disabled }: { groupId: string; disabled: boolean }) {
  const { message } = App.useApp();
  const all = useMembers(true);
  const inGroup = useGroupMembers(groupId);
  const move = useGroupMembership();
  if (!all.data || !inGroup.data) return <Skeleton active />;
  const targetKeys = inGroup.data.map((m) => m.userId);
  return (
    <Transfer
      showSearch
      disabled={disabled || move.isPending}
      titles={["Organization members", "In this group"]}
      dataSource={all.data.map((m) => ({
        key: m.userId,
        title: m.name || m.email,
        email: m.email,
      }))}
      targetKeys={targetKeys}
      filterOption={(q, item) =>
        `${item.title} ${item.email}`.toLowerCase().includes(q.toLowerCase())
      }
      render={(item) => `${item.title} (${item.email})`}
      onChange={(_next, direction, moved) =>
        move.mutate(
          {
            id: groupId,
            add: direction === "right" ? moved.map(String) : [],
            remove: direction === "left" ? moved.map(String) : [],
          },
          {
            onError: (e) =>
              message.error(
                describeApiError(e, {
                  byCode: ACCESS_ERROR_TEXT,
                  fallback: "Could not update the group's members.",
                }),
              ),
          },
        )
      }
    />
  );
}
