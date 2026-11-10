# sessions

**Sessions** (`/sessions`) and the drill-down **`/sessions/:sessionId`** (not in the nav; the breadcrumb links back).

- List: a table, newest first, with spent / saved / likely saved in separate columns, each row linking to its detail.
  Filters are URL params: `?range=&from=&to=&client=&outcome=completed|abandoned|open`. The range is sent as
  RFC 3339 `[from, to)` bounds; paging follows the opaque cursor.
- Detail: header (client, models, duration, calls, spent / saved / likely, outcome), the steps in call order, and a
  right rail (input tokens per call, repeats, a copyable prompt suggestion).
- Deep links name a call or an event by id: `?event=<eventId>` or `?call=<callId>`, never by call index (it restarts).
  The step is highlighted (`aria-current="step"`).
- A fired step carries an evidence card: monitor, action, certain or likely, the money, matched steps (linked by call
  id; an index of 0 or below reads "earlier call" and is not linked), tool, stored path or its 8-hex hash prefix, count
  against threshold, and the thumbs up/down buttons.
- A blocked step shows "saved $x" instead of a cost, and "Answered by the proxy: pointed the agent back to call #N's
  result" (nothing is replayed, no response body is stored; ruling S10).
- Feedback: `POST /api/workspaces/{id}/monitor-events/{eventId}/feedback` with `correct`, `wrong` or `null` (clicking the
  pressed one clears it); the detail is refetched so the stored value shows.
- Endpoints (`api/sessions.ts`): `GET /api/workspaces/{id}/sessions`, `GET /api/sessions/{id}?workspaceId=`.
- Permission: `session:read` on the active workspace (feedback needs the same). Without it: no-access and no request.
- Tests: `Page.test.tsx`, `SessionDetailPage.test.tsx`, `routes.test.tsx` (through the real routes and nav).
