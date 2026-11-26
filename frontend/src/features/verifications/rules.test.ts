import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { decisionError, loadError, reasonProblem } from './rules';

describe('reasonProblem', () => {
  it('needs a reason of 1–2000 UTF-8 bytes after trimming, like the API', () => {
    expect(reasonProblem('  ')).toBe('Give a reason; the student sees it and can correct their details.');
    expect(reasonProblem('  The student ID does not match our records.  ')).toBeUndefined();
    expect(reasonProblem('a'.repeat(2000))).toBeUndefined();
    expect(reasonProblem('a'.repeat(2001))).toBe('This reason is too long. Shorten it to about 2000 characters.');
    // "é" is two bytes in UTF-8, so 1001 of them are over the limit.
    expect(reasonProblem('é'.repeat(1001))).toBeDefined();
    expect(reasonProblem('ok\u0000')).toBe('The reason contains a character that is not allowed.');
  });
});

describe('error messages', () => {
  it('explains another faculty, a malformed link and an earlier decision', () => {
    const otherFaculty = 'This verification doesn’t exist or belongs to another faculty.';
    expect(loadError(new ApiError(404, 'resource not found'))).toBe(otherFaculty);
    expect(loadError(new ApiError(422, 'invalid input'))).toBe(otherFaculty);
    expect(decisionError(new ApiError(409, 'conflicting operation'))).toBe(
      'This verification was already decided, for example by another coordinator. The page now shows its current state.',
    );
    expect(decisionError(new ApiError(422, 'invalid input: a rejection reason of 1 to 2000 bytes is required'))).toBe(
      'A rejection reason of 1 to 2000 bytes is required',
    );
  });
});
