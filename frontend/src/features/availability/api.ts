import { api, type Page } from '../../shared/api/client';
import type { NewWindow, Rule, RulePatch, SkippedDate, Window } from './types';

const BASE = '/tutors/me/availability';
const id = (value: string) => encodeURIComponent(value);

/** The tutor's own availability (requires an active tutor profile; identity comes from the session). */
export const availabilityApi = {
  windows: (from: Date, to: Date, offset = 0, signal?: AbortSignal) =>
    api.get<Page<Window>>(BASE, { query: { from: from.toISOString(), to: to.toISOString(), limit: 100, offset }, signal }),
  addWindow: (window: NewWindow) => api.post<{ data: Window }>(`${BASE}/slots`, window),
  /** A recurring window becomes a skipped date of its rule. 409 if an active session depends on it. */
  removeWindow: (windowId: string) => api.delete(`${BASE}/slots/${id(windowId)}`),

  rules: (signal?: AbortSignal) => api.get<Page<Rule>>(`${BASE}/rules`, { query: { limit: 100 }, signal }),
  updateRule: (ruleId: string, patch: RulePatch) => api.patch<{ data: Rule }>(`${BASE}/rules/${id(ruleId)}`, patch),
  deleteRule: (ruleId: string) => api.delete(`${BASE}/rules/${id(ruleId)}`),
  skippedDates: (ruleId: string, signal?: AbortSignal) =>
    api.get<Page<SkippedDate>>(`${BASE}/rules/${id(ruleId)}/exceptions`, { query: { limit: 100 }, signal }),
  restoreDate: (ruleId: string, localDate: string) =>
    api.delete(`${BASE}/rules/${id(ruleId)}/exceptions/${id(localDate)}`),
};
