export interface StageMeta {
  stage: string;
  label: string;
  about: string;
}

/** The five stages in the order the server reports them, with a plain-words label and what each one does. */
export const STAGES: readonly StageMeta[] = [
  {
    stage: "repeat_collapse",
    label: "Repeated results",
    about:
      "A tool result the agent already got earlier becomes a one-line pointer to the first one.",
  },
  {
    stage: "dup_lines",
    label: "Duplicate lines",
    about:
      "Runs of identical or near-identical lines (progress output) become one line with a count.",
  },
  {
    stage: "log_window",
    label: "Long logs and diffs",
    about: "Keeps the start, the end and every error of a long log or diff; the middle is folded.",
  },
  {
    stage: "json_table",
    label: "JSON arrays",
    about: "A list of flat JSON objects becomes a compact table with the column names once.",
  },
  {
    stage: "schema_trim",
    label: "Long tool descriptions",
    about:
      "Shortens very long tool descriptions. Only in Aggressive mode; it changes the start of the request once.",
  },
];

export const stageLabel = (s: string): string => STAGES.find((m) => m.stage === s)?.label ?? s;
