import { describe, expect, it } from "vitest";
import { cleanTitle, dayShort, durationS, isClaude, money, pipClass, shortPath, timeHM, tokensHuman, waitMs } from "./format";

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

describe("cleanTitle", () => {
  it("drops pasted-content wrappers, @file references and separator runs", () => {
    expect(cleanTitle('<pasted_content id="8778"> pull latest from main </pasted_content id="8778">')).toBe("pull latest from main");
    expect(cleanTitle('@"/Users/me/Downloads/x.json" what is this')).toBe("what is this");
    expect(cleanTitle("#!/bin/bash # ============ go")).toBe("#!/bin/bash # go");
  });

  it("keeps something to show when nothing is left", () => {
    expect(cleanTitle("<pasted_content>")).toBe("<pasted_content>");
    expect(cleanTitle("")).toBe("(untitled session)");
  });
});

describe("shortPath", () => {
  it("replaces a home directory with ~", () => {
    expect(shortPath('cd "/Users/s.m/Documents/team/repo" && ls')).toBe('cd "~/Documents/team/repo" && ls');
    expect(shortPath("/home/dev/app/main.go")).toBe("~/app/main.go");
    expect(shortPath("src/a.ts")).toBe("src/a.ts");
  });
});

describe("isClaude", () => {
  it("knows Claude Code's name and no other agent's", () => {
    expect(isClaude("Claude Code")).toBe(true);
    expect(isClaude("Codex")).toBe(false);
    expect(isClaude("Claudette")).toBe(false);
  });
});
