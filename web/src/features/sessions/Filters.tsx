import { Flex, Select, theme } from "antd";
import { useSearchParams } from "react-router-dom";
import { useRangeParams } from "../../api/range";
import { CLIENT_FAMILIES, SESSION_OUTCOMES, type SessionFilters } from "../../api/sessions";
import { RangeControl } from "../../shell/RangeControl";

/** The session filters as URL search params (`?client=&outcome=` next to the range), so a reload keeps the view. */
export function useSessionFilters(): SessionFilters {
  const [params] = useSearchParams();
  const outcome = params.get("outcome") ?? "";
  return {
    client: params.get("client") ?? "",
    outcome: (SESSION_OUTCOMES as readonly string[]).includes(outcome) ? outcome : "",
  };
}

export function Filters({ filters }: { filters: SessionFilters }) {
  const { token } = theme.useToken();
  const { sel, setRange } = useRangeParams();
  const [, setParams] = useSearchParams();
  const set = (key: "client" | "outcome", value: string | undefined) =>
    setParams(
      (prev) => {
        const q = new URLSearchParams(prev);
        if (value) q.set(key, value);
        else q.delete(key);
        return q;
      },
      { replace: true },
    );
  return (
    <Flex gap={token.marginSM} wrap align="center">
      <RangeControl value={sel} onChange={setRange} />
      <Select
        allowClear
        placeholder="Client"
        aria-label="Client"
        style={{ minWidth: token.controlHeightLG * 4 }}
        value={filters.client || undefined}
        options={CLIENT_FAMILIES.map((c) => ({ value: c, label: c }))}
        onChange={(v) => set("client", v)}
      />
      <Select
        allowClear
        placeholder="Outcome"
        aria-label="Outcome"
        style={{ minWidth: token.controlHeightLG * 4 }}
        value={filters.outcome || undefined}
        options={SESSION_OUTCOMES.map((o) => ({ value: o, label: o }))}
        onChange={(v) => set("outcome", v)}
      />
    </Flex>
  );
}
