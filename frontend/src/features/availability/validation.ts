import type { SessionMode } from '../../shared/utils/datetime';
import { hhmm, localInstant } from './format';
import type { Rule, RulePatch } from './types';

/** Quick checks for clear messages. The server re-checks everything (semester, overlaps, DST gaps). */

export interface OneOffValues {
  date: string;
  start: string;
  end: string;
  mode: SessionMode;
}

export type OneOffErrors = Partial<Record<keyof OneOffValues, string>>;

export function validateOneOff(values: OneOffValues, now = new Date()): OneOffErrors {
  const errors: OneOffErrors = {};
  if (!values.date) errors.date = 'Choose a date.';
  if (!values.start) errors.start = 'Choose a start time.';
  if (!values.end) errors.end = 'Choose an end time.';
  if (errors.date || errors.start || errors.end) return errors;

  const start = localInstant(values.date, values.start);
  const end = localInstant(values.date, values.end);
  if (!start) errors.start = 'Enter a valid start time.';
  else if (start <= now) errors.start = 'Choose a time in the future.';
  if (!end) errors.end = 'Enter a valid end time.';
  else if (start && end <= start) errors.end = 'The end must be after the start.';
  return errors;
}

export interface RuleForm {
  /** ISO weekday as text ("1" = Monday). */
  weekday: string;
  start: string;
  end: string;
  zone: string;
  mode: SessionMode;
}

export function isTimeZone(zone: string): boolean {
  if (!zone.trim()) return false;
  try {
    new Intl.DateTimeFormat('en', { timeZone: zone.trim() });
    return true;
  } catch {
    return false;
  }
}

export function validateRule(form: RuleForm): Partial<Record<keyof RuleForm, string>> {
  const errors: Partial<Record<keyof RuleForm, string>> = {};
  if (!/^\d{2}:\d{2}$/.test(form.start)) errors.start = 'Choose a start time.';
  if (!/^\d{2}:\d{2}$/.test(form.end)) errors.end = 'Choose an end time.';
  // Same-day local times ("HH:MM" compares correctly as text).
  else if (!errors.start && form.end <= form.start) errors.end = 'The end must be after the start on the same day.';
  if (!isTimeZone(form.zone)) errors.zone = 'Enter a time zone such as Europe/Luxembourg.';
  return errors;
}

/** Only the fields the tutor changed, so an edit never overwrites a value it didn't touch. */
export function changedFields(rule: Rule, form: RuleForm): RulePatch {
  const patch: RulePatch = {};
  if (Number(form.weekday) !== rule.iso_weekday) patch.iso_weekday = Number(form.weekday);
  if (form.start !== hhmm(rule.local_start_time)) patch.local_start_time = form.start;
  if (form.end !== hhmm(rule.local_end_time)) patch.local_end_time = form.end;
  if (form.zone.trim() !== rule.time_zone) patch.time_zone = form.zone.trim();
  if (form.mode !== rule.allowed_mode) patch.allowed_mode = form.mode;
  return patch;
}
