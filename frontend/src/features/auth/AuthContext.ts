import { createContext, useContext } from 'react';
import type { CurrentUser } from './types';

/**
 * Why a logged-in user became logged out:
 * - expired: the server no longer knows the session (401), e.g. it timed out.
 * - blocked: the account can't be used (403 on /auth/me), e.g. suspended or closed.
 * - signed-out: the user pressed "Log out". Only the Tutti session ended; the
 *   login page explains that the university account may still be signed in.
 */
export type SessionEnd = 'expired' | 'blocked' | 'signed-out';

export type AuthState =
  | { status: 'loading'; user: null }
  | { status: 'anonymous'; user: null; ended?: SessionEnd }
  | { status: 'unavailable'; user: null }
  | { status: 'authenticated'; user: CurrentUser };

export interface AuthContextValue {
  state: AuthState;
  user: CurrentUser | null;
    /** Development test accounts only (features/auth/devLogin.ts). 
     * Real users sign in with the university. */
  login: (email: string, password: string) => Promise<CurrentUser>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error('useAuth must be used inside <AuthProvider>');
  return value;
}
