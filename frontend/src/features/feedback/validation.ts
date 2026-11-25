import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import type { FeedbackInput } from './types';

/** The API counts characters as Unicode code points ("runes"), not UTF-16 units. */
export const COMMENT_MAX_CHARS = 2000;

export interface FeedbackErrors {
  rating?: string;
  comment?: string;
}

export function characterCount(text: string): number {
  return [...text].length;
}

/** Same rules as the API: a whole-number rating from 1 to 5 and an optional comment of up to 2000 characters. */
export function validateFeedback(rating: number | null, comment: string): FeedbackErrors {
  const errors: FeedbackErrors = {};
  if (rating === null || !Number.isInteger(rating) || rating < 1 || rating > 5) {
    errors.rating = 'Choose a rating from 1 to 5.';
  }
  const text = comment.trim();
  const count = characterCount(text);
  if (count > COMMENT_MAX_CHARS) {
    errors.comment = `Shorten the comment to ${COMMENT_MAX_CHARS} characters or fewer (it has ${count}).`;
  } else if (text.includes('\u0000')) {
    errors.comment = 'The comment contains a character that is not allowed.';
  }
  return errors;
}

/** Trims the comment and leaves it out when empty, as the API stores it. */
export function toFeedbackInput(rating: number, comment: string): FeedbackInput {
  const text = comment.trim();
  return text ? { rating, comment: text } : { rating };
}

/** A sentence for a failed submission. */
export function feedbackError(error: unknown): string {
  if (error instanceof ApiError && error.status === 422 && /completed/i.test(error.message)) {
    return 'Only completed sessions can be rated. This session may have been marked as a no-show.';
  }
  return errorMessage(error, {
    404: "We couldn't find this session.",
    409: 'You have already rated this session.',
  });
}
