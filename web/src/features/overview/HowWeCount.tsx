import { Card, Typography } from "antd";

/** The "How we count" explainer at the bottom of the Overview: what each number means and what it never includes. */
export function HowWeCount() {
  return (
    <Card title="How we count" aria-label="How we count">
      <Typography.Paragraph>
        <Typography.Text strong>Saved</Typography.Text> is certain: a monitor acted (it blocked a
        call or added a note) and we know how much that kept out of the prompt. A blocked call is
        answered by the proxy, so what it would have cost is counted as saved.
      </Typography.Paragraph>
      <Typography.Paragraph>
        <Typography.Text strong>Likely saved</Typography.Text> is an estimate for a monitor that
        acted but cannot prove the effect, for example a note the agent may or may not have
        followed. It is shown next to saved and is never added to it.
      </Typography.Paragraph>
      <Typography.Paragraph>
        <Typography.Text strong>Would save</Typography.Text> is what a monitor in observe mode
        found. Nothing was changed, so nothing was saved; it is what turning the monitor on could
        have done.
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        Money comes from each model's list price. Calls with no price are left out. Days are UTC
        days. The chart compares what you paid with what you would have paid without the saved
        amount.
      </Typography.Paragraph>
    </Card>
  );
}
