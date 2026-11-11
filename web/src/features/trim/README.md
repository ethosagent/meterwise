# trim (web)

Trimming on the web. `TrimEvent` is the card on a session step ("Trimmed −N tokens", certain or likely, or "Would
trim" in observe); its Details button opens `TrimPreviewDrawer`: per-stage sizes and the fixed marker each stage
writes, never request text. `TrimSettings` is the Trimming row of the Savers card: the mode (Aggressive waits behind
a confirm), the last 7 days (saved and likely saved apart; in observe, "would save $X"; never summed) and
`TrimSettingsDrawer` for stage switches, never-trim tools, max fold, least gain and the by-tool table. Queries are in
`api/trim.ts`; fixtures in `test/fixtures/trim.ts`.
