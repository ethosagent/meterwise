import { useMutation, useQueryClient } from "@tanstack/react-query";

/** Encodes one path segment. */
export const enc = encodeURIComponent;

/** A JSON request body. */
export const json = (v: unknown) => JSON.stringify(v);

type Keys = readonly (readonly unknown[])[];

/**
 * A mutation that refetches `keys` afterwards: on success, or with `settled` on failure too (for a
 * mutation that sends several requests and can half-apply). Callers name every key they touch; nothing
 * is added implicitly.
 */
export function useInvalidatingMutation<V, R>(
  fn: (v: V) => Promise<R>,
  keys: Keys,
  { settled = false }: { settled?: boolean } = {},
) {
  const qc = useQueryClient();
  const refresh = async () => {
    await Promise.all(keys.map((queryKey) => qc.invalidateQueries({ queryKey })));
  };
  return useMutation(
    settled ? { mutationFn: fn, onSettled: refresh } : { mutationFn: fn, onSuccess: refresh },
  );
}
