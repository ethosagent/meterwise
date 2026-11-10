import { Button, Flex, theme } from "antd";

/** The "Load more" button under a paged table; renders nothing once the last page is in. */
export function LoadMore({
  hasMore,
  loading,
  onMore,
}: {
  hasMore: boolean;
  loading: boolean;
  onMore: () => void;
}) {
  const { token } = theme.useToken();
  if (!hasMore) return null;
  return (
    <Flex justify="center" style={{ marginTop: token.marginSM }}>
      <Button onClick={onMore} loading={loading}>
        Load more
      </Button>
    </Flex>
  );
}
