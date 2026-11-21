import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { ApiError, onUnauthorized } from '../../shared/api/client';
import { authApi } from './api';
import { AuthContext, type AuthContextValue, type AuthState } from './AuthContext';

/**
 * Keeps track of who is logged in.
 * - On page load it asks GET /auth/me, so a refresh restores the session.
 * - When the tab becomes visible again it re-checks, so roles, verification
 *   and suspension changes made by a coordinator show up.
 * - Any 401 from any API call marks the user as logged out.
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
            setState({ status: 'anonymous', user: null });
          } else {
            // Keep an existing session on a temporary failure; only block the first load.
            setState((previous) => (previous.status === 'loading' ? { status: 'unavailable', user: null } : previous));
          }
        },
      ),
    [],
  );

  useEffect(() => {
    onUnauthorized(() => setState({ status: 'anonymous', user: null }));
    void refresh();

    const onVisible = () => {
      if (document.visibilityState === 'visible') void refresh();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      onUnauthorized(null);
      document.removeEventListener('visibilitychange', onVisible);
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
      setState({ status: 'anonymous', user: null });
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ state, user: state.user, login, logout, refresh }),
    [state, login, logout, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
