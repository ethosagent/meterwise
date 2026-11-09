import { useQuery } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderAt } from "../test/render";
import { enc, json, useInvalidatingMutation } from "./mutation";

const KEY = ["probe"] as const;

function Probe({ fail, settled }: { fail: boolean; settled?: boolean }) {
  const fetches = useQuery({ queryKey: KEY, queryFn: () => Promise.resolve(Date.now()) });
  const run = useInvalidatingMutation(
    async () => {
      if (fail) throw new Error("nope");
    },
    [KEY],
    settled === undefined ? undefined : { settled },
  );
  return (
    <>
      <output>{fetches.dataUpdatedAt}</output>
      <p>{run.status}</p>
      <button type="button" onClick={() => run.mutate(undefined, { onError: () => undefined })}>
        go
      </button>
    </>
  );
}

async function updatedAfterClick(ui: React.ReactElement) {
  renderAt(ui);
  const out = await screen.findByRole("status");
  await waitFor(() => expect(out.textContent).not.toBe("0"));
  const before = out.textContent;
  await userEvent.click(screen.getByRole("button", { name: "go" }));
  // The mutation settles only after its callbacks (the invalidation) are awaited.
  await screen.findByText(/^(success|error)$/);
  await waitFor(() => expect(screen.getByRole("status").textContent).toBeTruthy());
  return { before, after: screen.getByRole("status").textContent };
}

describe("useInvalidatingMutation", () => {
  it("refetches the keys after a success", async () => {
    const { before, after } = await updatedAfterClick(<Probe fail={false} />);
    expect(after).not.toBe(before);
  });

  it("leaves the keys alone after a failure by default", async () => {
    const { before, after } = await updatedAfterClick(<Probe fail />);
    expect(after).toBe(before);
  });

  it("refetches after a failure when settled is set (a half-applied sequence)", async () => {
    const { before, after } = await updatedAfterClick(<Probe fail settled />);
    expect(after).not.toBe(before);
  });
});

describe("helpers", () => {
  it("enc encodes a path segment and json stringifies a body", () => {
    expect(enc("a/b c")).toBe("a%2Fb%20c");
    expect(json({ a: 1 })).toBe('{"a":1}');
  });
});
