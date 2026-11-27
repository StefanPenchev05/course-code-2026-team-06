import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { ApiError, onForbidden, onUnauthorized } from '../../shared/api/client';
import { authApi } from './api';
import { AuthContext, type AuthContextValue, type AuthState } from './AuthContext';

/** Logged out by the server. Only says why if the user was logged in; a failed login is not an expired session. */
function endedState(previous: AuthState, reason: 'expired' | 'blocked'): AuthState {
  if (previous.status === 'authenticated') return { status: 'anonymous', user: null, ended: reason };
  if (previous.status === 'anonymous') return previous;
  // First page load: a leftover cookie of a blocked account is worth explaining; an expired one isn't.
  return reason === 'blocked' ? { status: 'anonymous', user: null, ended: reason } : { status: 'anonymous', user: null };
}

/**
 * Keeps track of who is logged in.
 * - On page load it asks GET /auth/me, so a refresh restores the session.
 * - When the tab becomes visible again it re-checks, so roles, verification
 *   and suspension changes made by a coordinator show up.
 * - Any 401 from any API call means the session ended (e.g. expired).
 * - A 403 from GET /auth/me means the account can't be used any more
 *   (suspended or closed); any other 403 triggers that check.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading', user: null });

  // State is only updated in the response callbacks, never synchronously,
  // so calling refresh() from an effect doesn't cause cascading renders.
  const refresh = useCallback(
    (): Promise<void> =>
      authApi.me().then(
        (user) => setState({ status: 'authenticated', user }),
        (error: unknown) => {
          if (error instanceof ApiError && error.status === 401) {
            setState((previous) => endedState(previous, 'expired'));
          } else if (error instanceof ApiError && error.status === 403) {
            setState((previous) => endedState(previous, 'blocked'));
          } else {
            // Keep an existing session on a temporary failure; only block the first load.
            setState((previous) => (previous.status === 'loading' ? { status: 'unavailable', user: null } : previous));
          }
        },
      ),
    [],
  );

  useEffect(() => {
    onUnauthorized(() => setState((previous) => endedState(previous, 'expired')));
    onForbidden((path) => {
      // /auth/me is the check itself; login and register 403s happen while logged out.
      if (path !== '/auth/me' && path !== '/auth/login' && path !== '/auth/register') void refresh();
    });
    void refresh();

    const onVisible = () => {
      if (document.visibilityState === 'visible') void refresh();
    };
        // Back from the university sign-in page can restore an old copy of the page: re-check the session.
        const onPageShow = (event: PageTransitionEvent) => {
          if (event.persisted) void refresh();
        };
        document.addEventListener('visibilitychange', onVisible);
        window.addEventListener('pageshow', onPageShow);
        return () => {
          onUnauthorized(null);
          onForbidden(null);
          document.removeEventListener('visibilitychange', onVisible);
          window.removeEventListener('pageshow', onPageShow);
        };
  }, [refresh]);

  const login = useCallback(async (email: string, password: string) => {
    const user = await authApi.login(email, password);
    setState({ status: 'authenticated', user });
    return user;
  }, []);

  const logout = useCallback(async () => {
    try {
      await authApi.logout();
    } catch (error) {
      // Already logged out on the server is fine; anything else still ends the local session.
      if (!(error instanceof ApiError)) throw error;
    } finally {
      // Logging out on purpose: no "session ended" warning, but the login page
      // explains that this doesn't sign the user out of the university account.
      setState({ status: 'anonymous', user: null, ended: 'signed-out' });
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ state, user: state.user, login, logout, refresh }),
    [state, login, logout, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
