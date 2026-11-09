# 0008. Web design tokens derived from the plan and prototype

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 01 §2 and §7, controller ruling R14 (`web-design.md` from the Decodx program is not on this machine)

## Context

Plan 01 says the shell follows `web-design.md` §2-4: tokens, antd `ConfigProvider`, a dark sider in both themes, full-width content. That file could not be found, so the tokens cannot be copied. What survives is the plan's constraints (dark collapsible sider in both themes, antd tokens only, no hardcoded hex) and the Customer Dashboard prototype (`.superpowers/sdd/00-overview/design/customer-dashboard.html`: navy surfaces, blue accent, green/amber/red signals). Options: block on the missing file; invent a palette freely; derive from the prototype (chosen).

## Decision

`web/src/theme/theme.ts` is the one file allowed to hold colour literals. Dark values are the prototype's (accent `#4f8cff`, success `#2fd48a`, warning `#ffb347`, error `#ff5c7a`, surfaces `#0b0f17` / `#111726` / `#182034`, border `#243049`, text `#e6eaf2`). Light values are the same hues darkened until text and status colours reach 4.5:1 on white (measured with the WCAG formula: primary 4.9, success 5.4, warning 5.4, error 5.0, secondary text 5.8). On the dark primary, white text would be 3.2:1, so in dark mode text on a primary fill uses the page background colour (5.6:1; the brand mark in the sider does the same). The sider uses one navy surface and antd's dark algorithm through a nested `ConfigProvider`, so it is identical in both themes. Radius is 8, font is the system stack, and layout sizes (sider 248 / 72, header 56) are exported constants. Components read `theme.useToken()` only. Reduced motion sets antd's `motion` token to `false`.

H5 audit for this plan: every declared npm package (react, react-dom, react-router-dom, antd, @ant-design/icons, @tanstack/react-query, zustand, @vitejs/plugin-react, jsdom, @testing-library/react, jest-dom, user-event, @types/react, @types/react-dom) was already on the allowlist in `internal/archtest/scan_test.go`, so no allowlist entry was added. antd is 6.x because 5.x logs a compatibility warning on React 19.

## Consequences

Re-theming is a one-file change; a missing `web-design.md` is a palette review, not a rewrite. The values are an inference: when the real file turns up, supersede this ADR. Contrast was computed with a script, not an audit tool; hover and disabled states are antd's derivations and unchecked. Enforced by: `src/shell/hygiene.test.ts` (no hex outside `theme.ts`, no product string in shipped sources), `src/theme/theme.test.ts` (dark sider in both themes), the `web` gate.
