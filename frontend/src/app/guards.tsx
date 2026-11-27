import { useEffect } from 'react';
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router';
import { useAuth } from '../features/auth/AuthContext';
import { safeRedirect } from '../features/auth/redirect';
import { takeReturnPath } from '../features/auth/sso';
import { accessKey, hasRole, type Role } from '../features/auth/types';
import { Alert, Button, PageLoader } from '../shared/ui';

function ServerUnavailable() {
  const { refresh } = useAuth();
  return (
    <Alert
      title="We can't reach Tutti right now"
      action={
        <Button variant="secondary" onClick={() => void refresh()}>
          Try again
        </Button>
      }
    >
      Check your connection. If you're developing locally, make sure the API is running.
    </Alert>
  );
}

/**
 * After the university sign-in the API always opens a fixed page (OIDC_POST_LOGIN_PATH, "/").
 * If the user had wanted another page, open it now, replacing the history entry so
 * Back doesn't bounce through the landing page. The path was checked by safePath().
 */
function useReturnAfterUniversitySignIn(authenticated: boolean) {
  const navigate = useNavigate();
  const { pathname, search } = useLocation();

  useEffect(() => {
    if (!authenticated) return;
    const returnTo = takeReturnPath();
    if (returnTo && returnTo !== pathname + search) navigate(returnTo, { replace: true });
  }, [authenticated, navigate, pathname, search]);
}

/** Pages only for logged-in users. Others are sent to /login and come back afterwards. */
export function RequireAuth() {
  const { state } = useAuth();
  const location = useLocation();
  useReturnAfterUniversitySignIn(state.status === 'authenticated');

  if (state.status === 'loading') return <PageLoader label="Checking your session…" />;
  if (state.status === 'unavailable') return <ServerUnavailable />;
  if (state.status === 'anonymous') return <Navigate to="/login" replace state={{ from: location }} />;
  // A change of roles, verification, program or faculty (#93) loads the page again from
  // scratch: no list, detail or form from the old access stays on screen.
  return <Outlet key={accessKey(state.user)} />;
}

/** Pages only for visitors who are not logged in (login, register). */
export function PublicOnly() {
  const { state } = useAuth();
  const location = useLocation();

  if (state.status === 'loading') return <PageLoader />;
  if (state.status === 'authenticated') return <Navigate to={safeRedirect(location.state)} replace />;
  return <Outlet />;
}

interface RequireRoleProps {
  role?: Role;
  /** Student features need an approved registration, not just a login. */
  verifiedStudent?: boolean;
}

/**
 * Hides screens the user can't use. This is only for a clear UI: the API
 * still checks every permission on the server.
 */
export function RequireRole({ role, verifiedStudent = false }: RequireRoleProps) {
  const { user } = useAuth();
  const allowed = user !== null && (!role || hasRole(user, role)) && (!verifiedStudent || user.verified_student);

  if (!allowed) {
    const message = verifiedStudent && user && !user.verified_student
      ? 'This becomes available once a coordinator approves your registration.'
      : "Your account doesn't have access to this page.";
    return (
      <section className="page">
        <h1>You don't have access to this page</h1>
        <p>{message}</p>
      </section>
    );
  }
  return <Outlet />;
}
