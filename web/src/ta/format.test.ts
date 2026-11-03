import { describe, expect, it } from "vitest";
import { dayShort, durationS, money, pipClass, timeHM, tokensHuman, waitMs } from "./format";

describe("format", () => {
  it("formats money to cents", () => {
    expect(money(0)).toBe("$0.00");
    expect(money(12.345)).toBe("$12.35");
  });

  it("shortens token counts", () => {
    expect(tokensHuman(512)).toBe("512");
    expect(tokensHuman(31_000)).toBe("31K");
    expect(tokensHuman(15_200_000)).toBe("15.2M");
  });

  it("formats durations in seconds", () => {
    expect(durationS(0)).toBe("—");
    expect(durationS(20)).toBe("1m");
    expect(durationS(240)).toBe("4m");
    expect(durationS(3900)).toBe("1h 05m");
  });

  it("formats waits in milliseconds", () => {
    expect(waitMs(0)).toBe("—");
    expect(waitMs(40_000)).toBe("40s");
    expect(waitMs(120_000)).toBe("2m");
    expect(waitMs(75_000)).toBe("1m 15s");
  });

  it("formats dates and times", () => {
    const t = new Date(2026, 9, 20, 9, 5).getTime();
    expect(dayShort(t)).toBe("Oct 20");
    expect(timeHM(t)).toBe("09:05");
  });

  it("maps severity to a pip class", () => {
    expect(pipClass("full")).toBe("full");
    expect(pipClass("half")).toBe("half");
    expect(pipClass("fine")).toBe("");
    expect(pipClass(undefined)).toBe("");
  });
});
