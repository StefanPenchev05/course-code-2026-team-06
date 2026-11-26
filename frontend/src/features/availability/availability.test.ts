import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { availabilityError } from './errors';
import { defaultSemester, localDateText, ruleText } from './format';
import type { Rule } from './types';
import { changedFields, validateOneOff, validateRule } from './validation';

const RULE: Rule = {
  id: 'r1',
  semester_id: 's1',
  iso_weekday: 1,
  local_start_time: '14:00',
  local_end_time: '17:00',
  time_zone: 'Europe/Luxembourg',
  allowed_mode: 'BOTH',
};

describe('ruleText', () => {
  it('shows the local times with their zone, without seconds', () => {
    expect(ruleText({ ...RULE, local_start_time: '14:00:00', local_end_time: '17:00:00' })).toBe(
      'Every Monday, 14:00–17:00 (Europe/Luxembourg) · Online or on site',
    );
    expect(ruleText(RULE)).toBe('Every Monday, 14:00–17:00 (Europe/Luxembourg) · Online or on site');
    expect(ruleText({ ...RULE, iso_weekday: 7, allowed_mode: 'ONSITE' })).toBe('Every Sunday, 14:00–17:00 (Europe/Luxembourg) · On site');
  });
});

describe('localDateText', () => {
  it('shows a calendar date without shifting it by a time zone', () => {
    expect(localDateText('2026-12-07', 'en-GB')).toBe('Monday, 7 December 2026');
  });
});

describe('validateOneOff', () => {
  const now = new Date('2030-01-01T12:00:00');

  it('accepts a future window', () => {
    expect(validateOneOff({ date: '2030-01-07', start: '10:00', end: '11:30', mode: 'ONLINE' }, now)).toEqual({});
  });

  it('requires every field', () => {
    expect(Object.keys(validateOneOff({ date: '', start: '', end: '', mode: 'ONLINE' }, now)).sort()).toEqual(['date', 'end', 'start']);
  });

  it('rejects past times and an end before the start', () => {
    expect(validateOneOff({ date: '2029-12-31', start: '10:00', end: '11:00', mode: 'ONLINE' }, now).start).toBe('Choose a time in the future.');
    expect(validateOneOff({ date: '2030-01-07', start: '11:00', end: '10:00', mode: 'ONLINE' }, now).end).toBe('The end must be after the start.');
  });
});

describe('validateRule and changedFields', () => {
  const form = { weekday: '1', start: '14:00', end: '17:00', zone: 'Europe/Luxembourg', mode: 'BOTH' as const };

  it('checks times and the time zone', () => {
    expect(validateRule(form)).toEqual({});
    expect(validateRule({ ...form, end: '13:00' }).end).toMatch(/after the start/);
    expect(validateRule({ ...form, zone: 'Mars/Olympus' }).zone).toMatch(/time zone/);
  });

  it('sends only what changed', () => {
    expect(changedFields(RULE, form)).toEqual({});
    // The real API returns seconds ("14:00:00"); that is not a change.
    expect(changedFields({ ...RULE, local_start_time: '14:00:00', local_end_time: '17:00:00' }, form)).toEqual({});
    expect(changedFields(RULE, { ...form, weekday: '3', end: '18:00' })).toEqual({ iso_weekday: 3, local_end_time: '18:00' });
  });
});

describe('availabilityError', () => {
  it('explains each kind of conflict', () => {
    expect(availabilityError(new ApiError(409, 'an active session depends on this availability'))).toMatch(/booking that depends on this time/);
    expect(availabilityError(new ApiError(409, 'availability overlaps an existing window'))).toMatch(/overlaps another free time/);
    expect(availabilityError(new ApiError(409, 'availability changed concurrently; retry the operation'))).toMatch(/Reload the page/);
  });

  it('shows the rule behind a 422 without the technical prefix', () => {
    expect(availabilityError(new ApiError(422, 'invalid availability: window must fit inside one semester'))).toBe(
      'Window must fit inside one semester',
    );
  });
});

describe('defaultSemester', () => {
  const now = new Date('2030-03-01T12:00:00Z');
  const winter = { id: 'w', name: 'Winter', starts_at: '2029-09-01T00:00:00Z', ends_at: '2030-02-01T00:00:00Z' };
  const summer = { id: 's', name: 'Summer', starts_at: '2030-02-15T00:00:00Z', ends_at: '2030-07-01T00:00:00Z' };
  const autumn = { id: 'a', name: 'Autumn', starts_at: '2030-09-01T00:00:00Z', ends_at: '2031-02-01T00:00:00Z' };

  it('picks the running semester, else the next one', () => {
    expect(defaultSemester([autumn, summer], now)).toBe('s');
    expect(defaultSemester([autumn, winter], now)).toBe('w'); // earliest start when none runs
    expect(defaultSemester([], now)).toBe('');
  });
});
