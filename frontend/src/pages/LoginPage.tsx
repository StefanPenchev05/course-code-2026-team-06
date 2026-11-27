import { useEffect, useState } from 'react';
import { useLocation } from 'react-router';
import { useAuth, type SessionEnd } from '../features/auth/AuthContext';
import { DevPasswordLogin } from '../features/auth/DevPasswordLogin';
import { devPasswordLogin } from '../features/auth/devLogin';
import { safeRedirect } from '../features/auth/redirect';
import {
  browser,
  checkUniversitySignIn,
  clearAttempt,
  readAttempt,
  rememberAttempt,
  UNIVERSITY_LOGIN_PATH,
  type SignInProblem,
} from '../features/auth/sso';
import { useFocusOnFailure } from '../shared/hooks/useFocusOnFailure';
import { Alert, Button } from '../shared/ui';

interface Notice {
  kind: 'info' | 'warning';
  title: string;
  text: string;
}

/** Explains why the user is on the login page when they were logged out. */
const ENDED_NOTICE: Record<SessionEnd, Notice> = {
  expired: { kind: 'info', title: 'Your session ended', text: 'For your security you were logged out. Please log in again.' },
  blocked: {
    kind: 'warning',
    title: "This account can't be used right now",
    text: 'If it was suspended or closed, contact the coordinator of your faculty.',
  },
  // Tutti only ends its own session; it doesn't sign the user out at the university (no single logout).
  'signed-out': {
    kind: 'info',
    title: "You're logged out of Tutti",
    text: "This doesn't sign you out of your University of Luxembourg account. While that is still signed in, anyone using this browser may get back into Tutti without entering a password. On a shared computer, close all browser windows when you're done.",
  },
};

/** The user came back from the university page without being signed in to Tutti. */
const UNFINISHED_NOTICE: Notice = {
  kind: 'warning',
  title: "Signing in with your university account didn't finish",
  text: "If you cancelled, just try again. If it keeps failing, your university account may not be connected to a Tutti account yet, or your Tutti account may be suspended. Contact the coordinator of your faculty.",
};

const PROBLEM: Record<SignInProblem, { title: string; text: string }> = {
  disabled: {
    title: "University sign-in isn't available here",
    text: import.meta.env.DEV
      ? 'This Tutti server has university sign-in switched off. To try it, start the API with OIDC_ENABLED=true (see frontend/README.md, "Status of #68").'
      : 'This Tutti server has university sign-in switched off. Please try again later.',
  },
  busy: {
    title: 'Too many sign-in attempts',
    text: 'There were too many sign-in attempts from your network. Wait a minute, then try again.',
  },
  unavailable: {
    title: "Signing in isn't possible right now",
    text: "The university sign-in or Tutti can't be reached at the moment. Please try again in a few minutes.",
  },
  offline: {
    title: "We can't reach Tutti right now",
    text: 'Check your connection and try again.',
  },
};

/**
 * Login with the University of Luxembourg account (#68). Tutti never asks for
 * the university password: the button sends the browser through the Tutti
 * API to the university's own sign-in page, and the API brings it back to
 * the app with a session.
 */
export function LoginPage() {
  const { state } = useAuth();
  const location = useLocation();
  // Why the user was logged out (if they were). Hidden once they try again.
  const [ended, setEnded] = useState(state.status === 'anonymous' ? state.ended : undefined);
  // Reading is enough here; the attempt is cleared in the effect below.
  const [unfinished, setUnfinished] = useState(() => readAttempt() !== null);
  const [problem, setProblem] = useState<SignInProblem | null>(null);
  const [redirecting, setRedirecting] = useState(false);
  const { alertRef, signalFailure } = useFocusOnFailure();

  useEffect(() => {
    clearAttempt();
    // Back from the university page can restore this page from the browser's
    // cache exactly as it was left: busy button, no notice. Reset it.
    const onPageShow = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      setRedirecting(false);
      if (readAttempt() !== null) {
        clearAttempt();
        setUnfinished(true);
      }
    };
    window.addEventListener('pageshow', onPageShow);
    return () => window.removeEventListener('pageshow', onPageShow);
  }, []);

  function clearNotices() {
    setEnded(undefined);
    setUnfinished(false);
    setProblem(null);
  }

  async function handleSignIn() {
    clearNotices();
    setRedirecting(true);
    const found = await checkUniversitySignIn();
    if (found) {
      setProblem(found);
      setRedirecting(false);
      signalFailure();
      return;
    }
    // Comes back to the page the user wanted (see RequireAuth), never to an address from the URL.
    rememberAttempt(safeRedirect(location.state));
    browser.goTo(UNIVERSITY_LOGIN_PATH);
  }

  const notice = ended ? ENDED_NOTICE[ended] : unfinished ? UNFINISHED_NOTICE : null;

  return (
    <section className="page page--narrow">
      <h1>Log in</h1>
      {notice && (
        <Alert kind={notice.kind} title={notice.title}>
          {notice.text}
        </Alert>
      )}
      <div ref={alertRef} tabIndex={-1}>
        {problem && <Alert title={PROBLEM[problem].title}>{PROBLEM[problem].text}</Alert>}
      </div>
      <p>
        Students, tutors and faculty coordinators sign in with their University of Luxembourg account. You type your
        university password only on the university's own page, never in Tutti.
      </p>
      <Button className="sso-button" onClick={() => void handleSignIn()} loading={redirecting}>
        Sign in with University of Luxembourg
      </Button>
      <h2>Good to know</h2>
      <ul className="link-list">
        <li>
          Signing in works for Tutti accounts that are connected to a university account. If it doesn't finish, contact
          the coordinator of your faculty.
        </li>
        <li>
          Waiting for approval? You can still sign in and see the status of your registration. Booking tutors opens once a
          coordinator approves it.
        </li>
        <li>
          Faculty coordinators: your coordinator tools come from Tutti's records for your faculty, not from your university
          account. If they're missing after you sign in, ask the Tutti administrators.
        </li>
        <li>Logging out of Tutti doesn't sign you out of your university account.</li>
      </ul>
      {import.meta.env.DEV && devPasswordLogin() && <DevPasswordLogin onSubmit={clearNotices} />}
    </section>
  );
}
