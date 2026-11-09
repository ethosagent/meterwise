import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// Under load the first render in a file (cold antd) and parallel files stretch async waits well past
// the 1000 ms default; a real break still fails, after 5 s instead of 1 s.
configure({ asyncUtilTimeout: 5000 });

// jsdom lacks matchMedia and ResizeObserver, which antd's responsive pieces need.
export function installMatchMedia(matches: (query: string) => boolean = () => false) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: (query: string) => ({
      matches: matches(query),
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    }),
  });
}

class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

installMatchMedia();
vi.stubGlobal("ResizeObserver", NoopResizeObserver);

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
  vi.stubGlobal("ResizeObserver", NoopResizeObserver);
});
