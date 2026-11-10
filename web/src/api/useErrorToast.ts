import { App } from "antd";
import { describeApiError } from "./errors";

/**
 * The one failure toast: `byCode` maps known error codes to sentences, a 422 shows its first
 * problem, anything else uses the caller's `fallback`.
 */
export function useErrorToast(
  byCode?: Record<string, string>,
): (e: unknown, fallback: string) => void {
  const { message } = App.useApp();
  return (e, fallback) => message.error(describeApiError(e, { byCode: byCode ?? {}, fallback }));
}
