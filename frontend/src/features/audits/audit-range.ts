export const MAX_CUSTOM_RANGE_MS = 365 * 24 * 60 * 60 * 1000;

export type AuditCustomRange = {
  start: string;
  end: string;
};

export function localDateTimeToISO(value: string): string | null {
  if (!value) {
    return null;
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

export function inclusiveRangeFromLocal(startLocal: string, endLocal: string): AuditCustomRange | null {
  const start = localDateTimeToISO(startLocal);
  const end = localDateTimeToISO(endLocal);
  if (!start || !end) {
    return null;
  }
  return { start, end };
}

export function isValidCustomRange(start: string, end: string): boolean {
  const from = new Date(start).getTime();
  const to = new Date(end).getTime();
  return Number.isFinite(from) && Number.isFinite(to) && to > from && to - from <= MAX_CUSTOM_RANGE_MS;
}

/** Repository filters use an exclusive end bound (`created_at < end`). */
export function exclusiveEndISO(endInclusiveISO: string): string {
  const date = new Date(endInclusiveISO);
  if (Number.isNaN(date.getTime())) {
    return endInclusiveISO;
  }
  return new Date(date.getTime() + 1000).toISOString();
}
