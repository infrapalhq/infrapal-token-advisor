import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// jsdom doesn't lay pages out, so it has no scrolling.
Element.prototype.scrollIntoView = () => {};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
  delete document.documentElement.dataset.theme;
});
