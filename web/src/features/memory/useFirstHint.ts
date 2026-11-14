import { App } from "antd";
import { createElement, useEffect } from "react";
import { useNavigate } from "react-router-dom";

const key = (workspaceId: string) => `memory.firstHint.${workspaceId}`;

function seen(workspaceId: string): boolean {
  try {
    return window.localStorage.getItem(key(workspaceId)) !== null;
  } catch {
    return true; // no storage: never nag
  }
}

function remember(workspaceId: string) {
  try {
    window.localStorage.setItem(key(workspaceId), "1");
  } catch {
    // The toast just shows again next time.
  }
}

/** Celebrates the first hint memory gave in this workspace, once: a toast that links to the sessions it hinted. */
export function useFirstHintToast(workspaceId: string, injectedHits: number | undefined) {
  const { message } = App.useApp();
  const navigate = useNavigate();
  useEffect(() => {
    if (!injectedHits || seen(workspaceId)) return;
    remember(workspaceId);
    const to = "/sessions?tag=remembered";
    message.success({
      content: createElement(
        "span",
        null,
        "Memory gave its first hint. ",
        createElement(
          "a",
          {
            href: to,
            onClick: (e: { preventDefault: () => void }) => {
              e.preventDefault();
              navigate(to);
            },
          },
          "See the step",
        ),
      ),
    });
  }, [workspaceId, injectedHits, message, navigate]);
}
