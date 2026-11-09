import { describe, expect, it, vi } from "vitest";
import { permissionsKey } from "./access";
import { ApiError } from "./errors";
import { createQueryClient, resetForbiddenRefresh } from "./queryClient";

const refusal = () =>
  new ApiError(403, { code: "forbidden", message: "no", details: { reason: "no matching grant" } });

async function refuse(client: ReturnType<typeof createQueryClient>) {
  await client
    .getMutationCache()
    .build(client, {
      mutationFn: async () => {
        throw refusal();
      },
    })
    .execute(undefined)
    .catch(() => undefined);
}

describe("the 403 refresh dedupe", () => {
  it("refreshes once per snapshot, and again after an identity change even if the snapshot is identical", async () => {
    const client = createQueryClient(false);
    client.setQueryData(permissionsKey, { admin: false, grants: [] });
    const refresh = vi.spyOn(client, "invalidateQueries");

    await refuse(client);
    const afterFirst = refresh.mock.calls.length;
    expect(afterFirst).toBeGreaterThan(0);

    await refuse(client); // same snapshot: a real refusal, no second refresh
    expect(refresh.mock.calls.length).toBe(afterFirst);

    resetForbiddenRefresh(client); // another identity signs in
    client.setQueryData(permissionsKey, { admin: false, grants: [] }); // byte-identical snapshot
    await refuse(client);
    expect(refresh.mock.calls.length).toBeGreaterThan(afterFirst);
  });
});
