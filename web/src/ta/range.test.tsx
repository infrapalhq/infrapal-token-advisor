import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { useRange } from "./range";

const at = (path: string) => ({ wrapper: ({ children }: { children: ReactNode }) => <MemoryRouter initialEntries={[path]}>{children}</MemoryRouter> });

describe("useRange", () => {
  it("prefers the address, then the last choice, then 7 days", () => {
    expect(renderHook(() => useRange()[0], at("/?range=30d")).result.current).toBe("30d");
    localStorage.setItem("ta-range", "3m");
    expect(renderHook(() => useRange()[0], at("/")).result.current).toBe("3m");
    expect(renderHook(() => useRange()[0], at("/?range=bogus")).result.current).toBe("3m");
    localStorage.clear();
    expect(renderHook(() => useRange()[0], at("/")).result.current).toBe("7d");
  });

  it("still works when storage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(renderHook(() => useRange()[0], at("/")).result.current).toBe("7d");
  });
});
