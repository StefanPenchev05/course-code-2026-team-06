import { createContext, useContext } from 'react';
import type { CurrentUser } from './types';

export type AuthState =
  | { status: 'loading'; user: null }
  | { status: 'anonymous'; user: null }
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
