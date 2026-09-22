import { describe, expect, it } from "vitest";

import { exclusiveEndISO, inclusiveRangeFromLocal, isValidCustomRange, localDateTimeToISO } from "./audit-range";

describe("audit custom range", () => {
  it("converts local datetime strings to ISO", () => {
    const iso = localDateTimeToISO("2026-09-16T08:30:00");
    expect(iso).toMatch(/^\d{4}-\d{2}-\d{2}T/);
    expect(new Date(iso ?? "").getTime()).toBe(new Date("2026-09-16T08:30:00").getTime());
  });

  it("rejects inverted or empty ranges", () => {
    expect(inclusiveRangeFromLocal("", "2026-09-16T08:30:00")).toBeNull();
    expect(isValidCustomRange("2026-09-16T08:30:00.000Z", "2026-09-16T08:30:00.000Z")).toBe(false);
    expect(isValidCustomRange("2026-09-16T09:00:00.000Z", "2026-09-16T08:00:00.000Z")).toBe(false);
    expect(isValidCustomRange("2026-09-16T08:00:00.000Z", "2026-09-16T09:00:00.000Z")).toBe(true);
  });

  it("adds one second so the selected end second stays inclusive", () => {
    expect(exclusiveEndISO("2026-09-16T08:30:59.000Z")).toBe("2026-09-16T08:31:00.000Z");
  });
});
