import { api } from '../../shared/api/client';
import type { CurrentUser, RegistrationInput, StudentDetails, VerificationCreated } from './types';

// All authentication calls live here. Real users sign in with their university
// account (sso.ts, #68). login and register are the legacy password endpoints,
// used only by the development test-account forms (devLogin.ts).
export const authApi = {
  me: () => api.get<CurrentUser>('/auth/me'),
  login: (email: string, password: string) => api.post<CurrentUser>('/auth/login', { email, password }),
  logout: () => api.post('/auth/logout'),
  register: (input: RegistrationInput) => api.post<VerificationCreated>('/auth/register', input),
  /** Only for a logged-in student whose latest verification was REJECTED. */
  resubmitVerification: (details: StudentDetails) =>
    api.post<VerificationCreated>('/auth/resubmit-verification', details),
};
