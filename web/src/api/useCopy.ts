import { App } from "antd";

/** Copies `text` to the clipboard and toasts "<label> copied", or asks the person to copy it by hand. */
export function useCopy(text: string, label: string): () => Promise<void> {
  const { message } = App.useApp();
  return async () => {
    try {
      await navigator.clipboard.writeText(text);
      message.success(`${label} copied`);
    } catch {
      message.error(`Could not copy the ${label}. Select it and copy it by hand.`);
    }
  };
}
