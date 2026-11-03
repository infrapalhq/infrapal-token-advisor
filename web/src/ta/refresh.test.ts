import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useRefresh } from "./refresh";

describe("useRefresh", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("counts up on every interval, and stops when the page goes away", () => {
    vi.useFakeTimers();
    const { result, unmount } = renderHook(() => useRefresh(1000));
    expect(result.current).toBe(0);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(result.current).toBe(3);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
