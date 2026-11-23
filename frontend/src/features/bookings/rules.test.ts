import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { bookingError, hasEnded, isLateCancellation, isWebLink, PURPOSES, startOptions, validMeetingLink } from './rules';

const iso = (date: Date) => date.toISOString();

describe('purposes', () => {
  it('match the backend: introduction 30 minutes, everything else 60/90/120', () => {
    expect(PURPOSES.find((p) => p.value === 'INTRODUCTION')!.minutes).toEqual([30]);
    for (const purpose of PURPOSES.filter((p) => p.value !== 'INTRODUCTION')) expect(purpose.minutes).toEqual([60, 90, 120]);
  });
});

describe('startOptions', () => {
  const now = new Date('2030-01-07T08:00:00Z');

  it('offers half-hour starts where the whole session fits', () => {
    const window = { starts_at: '2030-01-08T09:00:00Z', ends_at: '2030-01-08T11:00:00Z' };
    expect(startOptions(window, 60, now).map(iso)).toEqual([
      '2030-01-08T09:00:00.000Z',
      '2030-01-08T09:30:00.000Z',
      '2030-01-08T10:00:00.000Z',
    ]);
    expect(startOptions(window, 120, now).map(iso)).toEqual(['2030-01-08T09:00:00.000Z']);
    expect(startOptions(window, 30, now)).toHaveLength(4);
  });

  it('starts on the next half hour inside an oddly aligned window', () => {
    const window = { starts_at: '2030-01-08T09:10:00Z', ends_at: '2030-01-08T10:40:00Z' };
    expect(startOptions(window, 60, now).map(iso)).toEqual(['2030-01-08T09:30:00.000Z']);
  });

  it('keeps at least 3 hours of notice', () => {
    const window = { starts_at: '2030-01-07T09:00:00Z', ends_at: '2030-01-07T14:00:00Z' };
    // now 08:00 → earliest 11:05 → first half hour 11:30
    expect(startOptions(window, 60, now)[0].toISOString()).toBe('2030-01-07T11:30:00.000Z');
  });

  it('offers nothing when the session no longer fits', () => {
    expect(startOptions({ starts_at: '2030-01-07T09:00:00Z', ends_at: '2030-01-07T11:00:00Z' }, 60, now)).toEqual([]);
  });
});

describe('isLateCancellation', () => {
  const now = new Date('2030-01-07T12:00:00Z');
  it('is late below 24 hours, not at exactly 24 hours', () => {
    expect(isLateCancellation('2030-01-08T11:59:00Z', now)).toBe(true);
    expect(isLateCancellation('2030-01-08T12:00:00Z', now)).toBe(false);
  });
});

describe('bookingError', () => {
  it('explains overlaps and deadlines in plain words', () => {
    expect(bookingError(new ApiError(409, 'a participant already has a reservation in this interval'))).toMatch(/already have a session/);
    expect(bookingError(new ApiError(409, 'the session request has expired'))).toMatch(/expired/);
    expect(bookingError(new ApiError(422, 'invalid booking: booking requires at least three hours of notice'))).toMatch(/at least 3 hours/);
    expect(bookingError(new ApiError(422, 'invalid booking: session time and mode must fit the window and semester'))).toMatch(
      /no longer free/,
    );
    expect(bookingError(new ApiError(409, 'session cannot make this transition'))).toMatch(/changed in the meantime/);
  });
});

describe('isWebLink', () => {
  it('only accepts http and https', () => {
    expect(isWebLink('https://meet.example/abc')).toBe(true);
    expect(isWebLink('javascript:alert(1)')).toBe(false);
    expect(isWebLink('Room B-203')).toBe(false);
  });
});

describe('validMeetingLink', () => {
  it('follows the backend rule: http(s), a host, no spaces, no user info', () => {
    expect(validMeetingLink('https://teams.example/meet/123')).toBe(true);
    expect(validMeetingLink('teams.example/meet')).toBe(false);
    expect(validMeetingLink('https://user:pw@meet.example')).toBe(false);
    expect(validMeetingLink('https://meet.example/a b')).toBe(false);
    expect(validMeetingLink('ftp://meet.example')).toBe(false);
  });
});

describe('hasEnded', () => {
  it('allows completion from the scheduled end on', () => {
    const now = new Date('2030-01-07T12:00:00Z');
    expect(hasEnded('2030-01-07T12:00:00Z', now)).toBe(true);
    expect(hasEnded('2030-01-07T12:00:01Z', now)).toBe(false);
  });
});
