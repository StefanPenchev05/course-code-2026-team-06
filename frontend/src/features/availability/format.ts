import { MODE_LABEL } from '../../shared/utils/datetime';
import type { Rule, Semester } from './types';

export const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'] as const;

/**
 * "14:00:00" or "14:00" → "14:00". The API returns local times with seconds
 * ("14:00:00"), although docs/scheduling.md shows "14:00"; requests accept "HH:MM".
 */
export function hhmm(time: string): string {
  return time.slice(0, 5);
}

/** "Every Monday, 14:00–17:00 (Europe/Luxembourg) · Online or on site": local times in the rule's own zone. */
export function ruleText(rule: Rule): string {
  const day = WEEKDAYS[rule.iso_weekday - 1] ?? `Day ${rule.iso_weekday}`;
  const mode = MODE_LABEL[rule.allowed_mode] ?? rule.allowed_mode;
  return `Every ${day}, ${hhmm(rule.local_start_time)}–${hhmm(rule.local_end_time)} (${rule.time_zone}) · ${mode}`;
}

/** "2026-12-07" → "Monday 7 December 2026" (a calendar date, no time zone shift). */
export function localDateText(localDate: string, locale?: string): string {
  const [year, month, day] = localDate.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { timeZone: 'UTC', weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).format(
    new Date(Date.UTC(year, month - 1, day)),
  );
}

/**
 * The absolute instant of a date and clock time typed in the browser's time
 * zone. Returns null for an invalid entry.
 */
export function localInstant(date: string, time: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !/^\d{2}:\d{2}$/.test(time)) return null;
  const instant = new Date(`${date}T${time}`);
  return Number.isNaN(instant.getTime()) ? null : instant;
}

/** The semester running now, or else the next one to start. */
export function defaultSemester(semesters: Semester[], now = new Date()): string {
  const running = semesters.find((s) => new Date(s.starts_at) <= now && now < new Date(s.ends_at));
  const next = [...semesters].sort((a, b) => a.starts_at.localeCompare(b.starts_at))[0];
  return (running ?? next)?.id ?? '';
}

