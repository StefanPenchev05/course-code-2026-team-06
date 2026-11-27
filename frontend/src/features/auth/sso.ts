import { API_BASE } from '../../shared/api/client';
import { safePath } from './redirect';

/**
 * Sign-in with the University of Luxembourg account (#68), through the Go API's
 * OIDC flow (docs/identity.md, "OIDC Stage 3B").
 *
 * - The browser only ever goes to this fixed Tutti address. The API builds the
 *   provider URL (state, nonce, PKCE); React never builds it and never passes
 *   query parameters on, so a crafted link can't change where sign-in goes.
 * - React never sees a university password or any token. After sign-in the API
 *   sets the usual HttpOnly session cookie and redirects to a fixed app path.
 */
export const UNIVERSITY_LOGIN_PATH = `${API_BASE}/auth/oidc/login`;

/** Why the university sign-in can't start right now. */
export type SignInProblem = 'disabled' | 'busy' | 'unavailable' | 'offline';

/** Leaves the app for the sign-in page. A separate object so tests can replace it. */
export const browser = {
  goTo(url: string) {
    window.location.assign(url);
  },
};

/**
 * Asks the API whether sign-in can start, without leaving the app, so a problem
 * is shown here instead of as a raw error page. The API answers 302 (redirect
 * to the university) when everything works; with redirect: 'manual' the
 * browser doesn't follow it and reports an "opaque redirect".
 *
 * Note: this check starts a short-lived sign-in attempt on the server, and the
 * real navigation then replaces it (one attempt cookie per browser).
 */
export async function checkUniversitySignIn(): Promise<SignInProblem | null> {
  let response: Response;
  try {
    response = await fetch(UNIVERSITY_LOGIN_PATH, { redirect: 'manual', credentials: 'same-origin', cache: 'no-store' });
  } catch {
    return 'offline';
  }
  if (response.type === 'opaqueredirect') return null;
  if (response.status === 404) return 'disabled'; // OIDC_ENABLED is off on this server
  if (response.status === 429) return 'busy'; // too many attempts from this network
  return 'unavailable'; // 503: the university sign-in or Tutti's session store is down
}

// ---------------------------------------------------------------------------
// The sign-in attempt, remembered for this tab while the user is away at the
// university page: where to return to, and whether the attempt finished.
// sessionStorage only holds an app path and a time, never a token.

const ATTEMPT_KEY = 'tutti.universitySignIn';
/** The API's sign-in attempts expire after at most 10 minutes (OIDC_TRANSACTION_TTL). */
const ATTEMPT_TTL_MS = 10 * 60 * 1000;

export interface SignInAttempt {
  /** Safe in-app path to open after signing in. */
  returnTo: string;
  startedAt: number;
}

export function rememberAttempt(returnTo: string, now = Date.now()): void {
  const attempt: SignInAttempt = { returnTo: safePath(returnTo), startedAt: now };
  try {
    sessionStorage.setItem(ATTEMPT_KEY, JSON.stringify(attempt));
  } catch {
    // Storage blocked: sign-in still works, the user just lands on the home page.
  }
}

/** The attempt started in this tab in the last 10 minutes, if any. Doesn't remove it. */
export function readAttempt(now = Date.now()): SignInAttempt | null {
  let raw: string | null;
  try {
    raw = sessionStorage.getItem(ATTEMPT_KEY);
  } catch {
    return null;
  }
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as Partial<SignInAttempt>;
    if (typeof value.startedAt !== 'number' || now - value.startedAt > ATTEMPT_TTL_MS || value.startedAt > now) return null;
    // Checked again here: storage is not trusted input.
    return { returnTo: safePath(value.returnTo), startedAt: value.startedAt };
  } catch {
    return null;
  }
}

export function clearAttempt(): void {
  try {
    sessionStorage.removeItem(ATTEMPT_KEY);
  } catch {
    // nothing to clear
  }
}

/** After a successful sign-in: the page to open (once), or null when there was no attempt. */
export function takeReturnPath(now = Date.now()): string | null {
  const attempt = readAttempt(now);
  clearAttempt();
  return attempt ? attempt.returnTo : null;
}
