import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { applyTheme, currentTheme, useTheme } from "./theme";

describe("theme", () => {
  it("defaults to dark", () => {
    expect(currentTheme()).toBe("dark");
    document.documentElement.dataset.theme = "light";
    expect(currentTheme()).toBe("light");
  });

  it("toggles and remembers the choice", () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current[0]).toBe("dark");
    act(() => result.current[1]());
    expect(result.current[0]).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem("ta-theme")).toBe("light");
    act(() => result.current[1]());
    expect(localStorage.getItem("ta-theme")).toBe("dark");
  });

  it("still applies the theme when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    applyTheme("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });
});
