# monitors

**Monitors** (`/monitors`): what is on and off, and what each monitor has done.

- Shows: the six monitors in API order. Per row: a `Segmented` mode (Observe / Nudge / Block), fires, **Saved**,
  **Likely saved** and **Would save** as three separate columns (never summed; would-save is an amber outlined tag),
  the count marked wrong, a "Turn on" shortcut on an observe row that has would-save, and a **Thresholds** `Drawer`.
- Block sits behind a `Popconfirm` (it answers the call itself); the mode only changes once it is confirmed.
- `reasoning_loop` and `second_guessing` are observe-only in the UI: Nudge and Block are disabled with the tooltip
  "observe-only until tuned". The API still accepts every mode (Plan 06 ruling).
- The thresholds drawer sends only the fields that changed, as a partial PATCH body.
- Range (7 or 30 days) lives in the URL: `?range=30d`.
- Endpoints (`api/monitors.ts`): `GET /api/workspaces/{id}/monitors`, `PATCH` the same path (writes audit),
  `GET .../monitors/stats?range=7d|30d`.
- Permissions (`usePermission`, never a role): page needs `monitor_config:read` (else a no-access state and no
  request); changing a mode or threshold needs `monitor_config:write` (else every control is disabled and the page
  says why); the money and fire columns need `savings:read` (else dashes and no stats request).
- Mutations refresh the config, every stats range and the audit log. Failures go through `useErrorToast`.
- Tests: `Page.test.tsx` against `test/mockApi.ts` (savings routes in `test/mockSavings.ts`, data in `test/fixtures`).
