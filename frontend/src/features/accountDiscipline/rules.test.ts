import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { actorText, decisionError, reasonProblem } from './rules';

describe('reasonProblem', () => {
  it('needs a reason of 1–2000 UTF-8 bytes after trimming, like the API', () => {
    expect(reasonProblem('  ')).toBe('Give a reason; it is kept in the account’s history.');
    expect(reasonProblem('  Repeated confirmed no-shows this semester.  ')).toBeUndefined();
    expect(reasonProblem('a'.repeat(2000))).toBeUndefined();
    expect(reasonProblem('a'.repeat(2001))).toBe('This reason is too long. Shorten it to about 2000 characters.');
    // "é" is two bytes in UTF-8, so 1001 of them are over the limit.
    expect(reasonProblem('é'.repeat(1001))).toBeDefined();
    expect(reasonProblem('ok\u0000')).toBe('The reason contains a character that is not allowed.');
  });
});

describe('actorText', () => {
  it('names no person, only the kind of actor that recorded the event', () => {
    expect(actorText('SYSTEM', null)).toBe('Recorded automatically');
    expect(actorText('USER', '44444444-4444-4444-4444-444444444444')).toBe('Recorded by coordinator ref. 44444444');
    expect(actorText(null, null)).toBe('Recorded by an unknown actor (older record)');
  });
});

describe('decisionError', () => {
  it('explains the home-faculty rule and a state that changed meanwhile', () => {
    expect(decisionError(new ApiError(404, 'not found'))).toMatch(/current home faculty/);
    expect(decisionError(new ApiError(409, 'conflict'))).toMatch(/^The account’s state changed meanwhile/);
  });
});
