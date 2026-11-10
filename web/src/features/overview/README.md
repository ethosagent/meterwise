# overview

**Overview** (`/overview`): what the proxy saved, and where. Tabs: **Summary** and **Where it came from** (`?tab=causes`).

- Summary: the hero ("You saved $X" in green, its share of what you would have paid, tokens not sent) with
  **likely saved** under a dashed rule as its own number; a spend chart (dashed would-have-paid, solid paid, the saved
  band shaded; recharts, lazy-loaded); savings by cause (three bar styles: solid green certain, grey likely, amber
  outline would-save); observe-mode upsell callouts; biggest saves (paged, each row links to
  `/sessions/:id?event=<eventId>`); "How we count".
- Where it came from: one card per monitor, the per-client table (with the average-calls-per-session delta) and
  copyable prompt suggestions.
- Saved and likely are never summed anywhere. Money is a decimal string formatted with BigInt (`lib/money.ts`); the
  chart reads each day's fields as the server sent them (`chartSeries.ts`, no arithmetic).
- Empty states: `no_calls` links to Setup; `collecting` says monitors are observing and shows would-save.
  The `no_calls` state polls every 15 s, like Setup, so it flips by itself.
- Range and tab are URL search params (`?range=7d|30d|custom&from=&to=&tab=`), UTC days. A custom range the server
  would refuse (over 90 days, after today, from after to) falls back to 7d.
- Endpoints (`api/savings.ts`): `GET /api/workspaces/{id}/savings/{overview,causes,saves}`. Keys are
  `workspaceScoped("savings", wsId, view, rangeKey)`.
- Permission: `savings:read` on the active workspace. Without it: a no-access state and no savings request.
- Tests: `Page.test.tsx` (mock API + fixtures in `test/fixtures/savings.ts`), `chartSeries` and range unit tests.
