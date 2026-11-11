# router (web)

Model routing in the UI. The mode switch lives in the Savers card (`features/monitors`); this folder owns the rest.

- **`RouterPanel`** (under the routing row of the Savers card): the 7-day figures (saved, or would-save in observe;
  never summed), the provider-refused count, the guardrail (a warning `Alert` when the API says `warn`), the
  threshold (`InputNumber`, empty = the mode default, sent as 0) and the model-map `Table` (remove a row, add a
  suggested or default pair, reset to defaults). Needs `monitor_config:write` to change anything and `savings:read`
  for the figures (no figures request without it, none while the mode is off).
- **`RouteEvent`**: the step card ("Routed", from to target, saved vs the model asked for, score) with a thumbs-down on
  routed calls: `POST .../calls/{id}/router-feedback` with `worse`, or null to clear. The call carries no stored
  verdict, so the pressed state is the last POST's answer.
- Endpoints (`api/router.ts`): GET/PATCH `.../router`, GET `.../router/stats?range=`, POST `.../router-feedback`.
