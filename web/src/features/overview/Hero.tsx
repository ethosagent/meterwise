import { Card, Divider, Flex, Typography, theme } from "antd";
import type { SavingsHero } from "../../contract/generated";
import { formatBp, formatTokens, formatUsd, isPositiveMicro } from "../../lib/money";

/**
 * "You saved $X": the certain figure alone, green, with its share of what would have been paid and
 * the tokens not sent. Likely saved sits under a dashed rule as its own number. The two are never
 * summed, here or anywhere (ADR 0023): each reads straight from its own field.
 */
export function Hero({ hero }: { hero: SavingsHero }) {
  const { token } = theme.useToken();
  const unpriced = hero.unpricedCalls > 0;
  return (
    <Card aria-label="Savings summary">
      <Flex gap={token.marginXL} wrap align="flex-end">
        <div>
          <Typography.Text type="secondary">You saved</Typography.Text>
          <Typography.Title
            level={1}
            aria-label="You saved"
            data-testid="hero-saved"
            style={{ margin: 0, color: token.colorSuccess }}
          >
            {formatUsd(hero.savedUsd)}
          </Typography.Title>
        </div>
        <div>
          <Typography.Title level={3} style={{ margin: 0 }} data-testid="hero-share">
            {formatBp(hero.savedBp)}
          </Typography.Title>
          <Typography.Text type="secondary">of what you would have paid</Typography.Text>
        </div>
        <div>
          <Typography.Title level={3} style={{ margin: 0 }} data-testid="hero-tokens">
            {formatTokens(hero.savedTokens)}
          </Typography.Title>
          <Typography.Text type="secondary">tokens not sent</Typography.Text>
        </div>
      </Flex>
      <Divider dashed style={{ borderColor: token.colorBorder }} />
      <Flex gap={token.marginSM} align="baseline" wrap data-testid="hero-likely">
        <Typography.Text type="secondary">Likely saved</Typography.Text>
        <Typography.Text strong style={{ fontSize: token.fontSizeLG }}>
          {formatUsd(hero.likelySavedUsd)}
        </Typography.Text>
        {isPositiveMicro(hero.likelySavedUsd) ? (
          <Typography.Text type="secondary">
            about {formatTokens(hero.likelySavedTokens)} tokens, an estimate shown on its own and
            not counted above
          </Typography.Text>
        ) : null}
      </Flex>
      {unpriced ? (
        <Typography.Paragraph
          type="secondary"
          style={{ marginTop: token.marginSM, marginBottom: 0 }}
        >
          {hero.unpricedCalls} {hero.unpricedCalls === 1 ? "call has" : "calls have"} no price on
          file, so {hero.unpricedCalls === 1 ? "it is" : "they are"} left out of the money figures.
        </Typography.Paragraph>
      ) : null}
    </Card>
  );
}
