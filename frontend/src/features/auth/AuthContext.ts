import { createContext, useContext } from 'react';
import type { CurrentUser } from './types';

/**
 * Why a logged-in user became logged out without pressing "Log out":
 * - expired: the server no longer knows the session (401), e.g. it timed out.
 * - blocked: the account can't be used (403 on /auth/me), e.g. suspended or closed.
 */
export type SessionEnd = 'expired' | 'blocked';

export type AuthState =
  | { status: 'loading'; user: null }
  | { status: 'anonymous'; user: null; ended?: SessionEnd }
  | { status: 'unavailable'; user: null }
  | { status: 'authenticated'; user: CurrentUser };

export interface AuthContextValue {
  state: AuthState;
  user: CurrentUser | null;
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
