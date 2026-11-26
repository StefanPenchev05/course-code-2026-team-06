import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { decisionError, excerpt, isNoShowStatus, loadError, noteProblem } from './rules';

describe('noteProblem', () => {
  it('needs a note of 1–2000 UTF-8 bytes after trimming, like the API', () => {
    expect(noteProblem('  ')).toBe('Explain your decision; it is stored with the report.');
    expect(noteProblem('  Both participants confirmed it by email.  ')).toBeUndefined();
    expect(noteProblem('a'.repeat(2000))).toBeUndefined();
    expect(noteProblem('a'.repeat(2001))).toBe('This note is too long. Shorten it to about 2000 characters.');
    // "é" is two bytes in UTF-8, so 1001 of them are over the limit.
    expect(noteProblem('é'.repeat(1001))).toBeDefined();
    expect(noteProblem('ok\u0000')).toBe('The note contains a character that is not allowed.');
  });
});

describe('helpers', () => {
  it('accepts only the three API statuses', () => {
    expect(['PENDING', 'CONFIRMED', 'REJECTED'].every(isNoShowStatus)).toBe(true);
    expect(isNoShowStatus('pending')).toBe(false);
  });

  it('shows the first line of the evidence', () => {
    expect(excerpt('I waited 30 minutes.\nNobody came.')).toBe('I waited 30 minutes.…');
  });

  it('explains another faculty and a decision that is no longer possible', () => {
    expect(loadError(new ApiError(404, 'attendance resource not found'))).toBe(
      'This report doesn’t exist or concerns a session of another faculty.',
    );
    expect(decisionError(new ApiError(409, 'attendance report already exists, was decided or conflicts with the session outcome'))).toMatch(
      /^This report was already decided, or the session’s outcome no longer allows confirming it/,
    );
  });
});
