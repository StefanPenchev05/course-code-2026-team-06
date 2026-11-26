import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { descriptionProblem, reportError, reportReference } from './rules';

describe('descriptionProblem', () => {
  it('needs 1–2000 UTF-8 bytes after trimming, like the API', () => {
    expect(descriptionProblem(' \n ')).toBe('Describe the problem.');
    expect(descriptionProblem('  The tutor did not answer my messages.  ')).toBeUndefined();
    expect(descriptionProblem('a'.repeat(2000))).toBeUndefined();
    expect(descriptionProblem('a'.repeat(2001))).toBe('This description is too long. Shorten it to about 2000 characters.');
    // "é" is two bytes in UTF-8, so 1001 of them are over the limit.
    expect(descriptionProblem('é'.repeat(1001))).toBeDefined();
    expect(descriptionProblem('ok\u0000')).toBe('The description contains a character that is not allowed.');
  });
});

describe('reportError', () => {
  it('explains an ineligible account and a session that is not the student’s', () => {
    expect(reportError(new ApiError(403, 'forbidden'))).toMatch(/can’t send reports right now/);
    expect(reportError(new ApiError(404, 'session not found'))).toMatch(/isn’t one of yours/);
  });
});

describe('reportReference', () => {
  it('is the first eight characters of the ID, in capitals', () => {
    expect(reportReference('0a1b2c3d-4e5f-6789-abcd-ef0123456789')).toBe('0A1B2C3D');
  });
});
