import { App } from "antd";
import { ACCESS_ERROR_TEXT } from "./access";
import { describeApiError } from "./errors";

/** The one failure toast for access, group and invitation mutations: known conflicts read as sentences, the rest use `fallback`. */
export function useAccessToast(): (e: unknown, fallback: string) => void {
  const { message } = App.useApp();
  return (e, fallback) =>
    message.error(describeApiError(e, { byCode: ACCESS_ERROR_TEXT, fallback }));
}
