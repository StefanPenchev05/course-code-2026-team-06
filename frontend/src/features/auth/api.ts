import { api } from '../../shared/api/client';
import type { CurrentUser, RegistrationInput, StudentDetails, VerificationCreated } from './types';

// All authentication calls live here so the planned switch to Uni.lu SSO (#68)
// only touches this folder.
export const authApi = {
  me: () => api.get<CurrentUser>('/auth/me'),
  login: (email: string, password: string) => api.post<CurrentUser>('/auth/login', { email, password }),
  logout: () => api.post('/auth/logout'),
  register: (input: RegistrationInput) => api.post<VerificationCreated>('/auth/register', input),
  /** Only for a logged-in student whose latest verification was REJECTED. */
  resubmitVerification: (details: StudentDetails) =>
    api.post<VerificationCreated>('/auth/resubmit-verification', details),
};
