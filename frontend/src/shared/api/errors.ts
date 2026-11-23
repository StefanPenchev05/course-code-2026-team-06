import { ApiError } from './client';

const DEFAULT_MESSAGES: Record<number, string> = {
  0: 'Could not reach the server. Check your connection and try again.',
  400: 'The request was not valid. Please check what you entered.',
  401: 'Your session has ended. Please log in again.',
  403: "You don't have permission to do that.",
  404: "We couldn't find what you were looking for.",
  409: 'This was already changed by someone else. Refresh the page and try again.',
  413: 'What you entered is too long.',
  422: 'Some details are not valid. Please check the form.',
  503: 'The server took too long to respond. Please try again.',
};

const GENERIC = 'Something went wrong. Please try again.';

/**
 * Turns any thrown value into a sentence a user can act on.
 * Pass overrides for messages that make more sense on a specific screen,
 * e.g. errorMessage(err, { 401: 'Email or password is incorrect.' }).
 */
export function errorMessage(error: unknown, overrides: Partial<Record<number, string>> = {}): string {
  if (!(error instanceof ApiError)) return GENERIC;
  const override = overrides[error.status];
  if (override) return override;
  // 422 messages from the API describe which rule failed, so show them.
  if (error.status === 422 && error.message) {
    // Identity prefixes rule messages with "invalid input: "; drop it.
    const text = error.message.replace(/^invalid input:\s*/i, '');
    return text.charAt(0).toUpperCase() + text.slice(1);
  }
  return DEFAULT_MESSAGES[error.status] ?? GENERIC;
}
