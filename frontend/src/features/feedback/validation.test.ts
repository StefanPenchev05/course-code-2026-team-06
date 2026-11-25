import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { characterCount, COMMENT_MAX_CHARS, feedbackError, toFeedbackInput, validateFeedback } from './validation';

describe('validateFeedback', () => {
  it('needs a whole-number rating from 1 to 5', () => {
    expect(validateFeedback(null, '').rating).toBe('Choose a rating from 1 to 5.');
    expect(validateFeedback(0, '').rating).toBeDefined();
    expect(validateFeedback(6, '').rating).toBeDefined();
    expect(validateFeedback(4.5, '').rating).toBeDefined();
    for (const rating of [1, 2, 3, 4, 5]) expect(validateFeedback(rating, '')).toEqual({});
  });

  it('allows an empty comment and up to 2000 characters, counted like the API', () => {
    expect(validateFeedback(5, '   ')).toEqual({});
    expect(validateFeedback(5, 'a'.repeat(COMMENT_MAX_CHARS))).toEqual({});
    expect(validateFeedback(5, `  ${'a'.repeat(COMMENT_MAX_CHARS)}  `)).toEqual({});
    expect(validateFeedback(5, 'a'.repeat(COMMENT_MAX_CHARS + 1)).comment).toBe(
      'Shorten the comment to 2000 characters or fewer (it has 2001).',
    );
    // An emoji is one character for the API, although it is two UTF-16 units in JavaScript.
    expect(characterCount('👍👍')).toBe(2);
    expect(validateFeedback(5, '👍'.repeat(COMMENT_MAX_CHARS))).toEqual({});
  });

  it('rejects the NUL character, which the API refuses', () => {
    expect(validateFeedback(3, 'ok\u0000').comment).toBe('The comment contains a character that is not allowed.');
  });
});

describe('toFeedbackInput', () => {
  it('trims the comment and leaves it out when empty', () => {
    expect(toFeedbackInput(4, '  Clear explanations.  ')).toEqual({ rating: 4, comment: 'Clear explanations.' });
    expect(toFeedbackInput(5, ' \n ')).toEqual({ rating: 5 });
  });
});

describe('feedbackError', () => {
  it('explains the API answers a student can meet', () => {
    expect(feedbackError(new ApiError(409, 'session already has feedback'))).toBe('You have already rated this session.');
    expect(feedbackError(new ApiError(422, 'only currently completed sessions can receive feedback'))).toBe(
      'Only completed sessions can be rated. This session may have been marked as a no-show.',
    );
    expect(feedbackError(new ApiError(422, 'rating must be an integer from 1 to 5 and comment at most 2000 characters'))).toBe(
      'Rating must be an integer from 1 to 5 and comment at most 2000 characters',
    );
    expect(feedbackError(new ApiError(0, ''))).toBe('Could not reach the server. Check your connection and try again.');
  });
});
