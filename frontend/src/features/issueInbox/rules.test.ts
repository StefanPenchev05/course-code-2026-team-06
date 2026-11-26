import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { excerpt, isIssueStatus, loadError, noteProblem, reference, resolveError } from './rules';

describe('noteProblem', () => {
  it('needs a note of 1–2000 UTF-8 bytes after trimming, like the API', () => {
    expect(noteProblem('  ')).toBe('Write what you did or decided; it is stored with the report.');
    expect(noteProblem('  Talked to both participants.  ')).toBeUndefined();
    expect(noteProblem('a'.repeat(2000))).toBeUndefined();
    expect(noteProblem('a'.repeat(2001))).toBe('This note is too long. Shorten it to about 2000 characters.');
    // "é" is two bytes in UTF-8, so 1001 of them are over the limit.
    expect(noteProblem('é'.repeat(1001))).toBeDefined();
    expect(noteProblem('ok\u0000')).toBe('The note contains a character that is not allowed.');
  });
});

describe('excerpt', () => {
  it('shows the first line and marks that there is more', () => {
    expect(excerpt('Short report.')).toBe('Short report.');
    expect(excerpt('First line.\nSecond line.')).toBe('First line.…');
    expect(excerpt('a'.repeat(200))).toBe(`${'a'.repeat(140)}…`);
  });
});

describe('helpers', () => {
  it('accepts only the two API statuses', () => {
    expect(isIssueStatus('OPEN')).toBe(true);
    expect(isIssueStatus('RESOLVED')).toBe(true);
    expect(isIssueStatus('open')).toBe(false);
  });

  it('builds the same short reference the student saw', () => {
    expect(reference('0a1b2c3d-4e5f-6789-abcd-ef0123456789')).toBe('0A1B2C3D');
  });

  it('explains another faculty and an earlier resolution', () => {
    expect(loadError(new ApiError(404, 'issue resource not found'))).toBe('This report doesn’t exist or belongs to another faculty.');
    expect(resolveError(new ApiError(409, 'issue report conflicts with current data'))).toMatch(/^This report was already resolved/);
  });
});
