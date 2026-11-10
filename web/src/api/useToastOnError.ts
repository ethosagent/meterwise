import { useEffect } from "react";
import { useErrorToast } from "./useErrorToast";

/** Toasts a failed query once per error, through the one failure toast. */
export function useToastOnError(error: unknown, fallback: string): void {
  const toast = useErrorToast();
  // biome-ignore lint/correctness/useExhaustiveDependencies: `toast` is a new closure each render; only a new error should toast
  useEffect(() => {
    if (error) toast(error, fallback);
  }, [error]);
}
