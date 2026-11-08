---
name: demo-traffic
description: Replay committed synthetic request fixtures through a local workspace URL so the dashboard has data. Manual only; run when a demo or screenshot needs traffic.
disable-model-invocation: true
---

# Demo traffic

Rules first:
- Manual only. Needs a running `meterwised` and a workspace URL (set `METERWISE_DEMO_URL`).
- Fixtures in `testdata/demo/` are synthetic; never replay real prompts or put keys in them.
- The upstream must be a local fake (tollgate `testkit.FakeUpstream`), never a live provider.

## Steps

1. Start the stack and create a workspace; copy its proxy URL.
2. `METERWISE_DEMO_URL=http://127.0.0.1:8080/w/<id> make demo-traffic`
   (runs `.agents/skills/demo-traffic/scripts/replay.sh`).
3. Open the dashboard and check the session list.
