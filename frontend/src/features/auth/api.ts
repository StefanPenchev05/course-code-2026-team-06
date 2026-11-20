import { api } from '../../shared/api/client';
import type { CurrentUser } from './types';

// All authentication calls live here so the planned switch to Uni.lu SSO (#68)
// only touches this folder.
export const authApi = {
  me: () => api.get<CurrentUser>('/auth/me'),
  login: (email: string, password: string) => api.post<CurrentUser>('/auth/login', { email, password }),
  logout: () => api.post('/auth/logout'),
};
