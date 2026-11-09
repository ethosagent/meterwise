import { Card, Empty } from "antd";
import { type NavKey, pageByKey } from "./nav";
import { PageHead } from "./PageHead";

/** Page-head plus an Empty body: what every destination renders until its feature lands. */
export function StubPage({ navKey }: { navKey: NavKey }) {
  const page = pageByKey(navKey);
  return (
    <section aria-labelledby="page-title">
      <PageHead page={page} />
      <Card>
        <Empty description="Nothing here yet" />
      </Card>
    </section>
  );
}
