import { sessionsApi } from './api';
import { statusLabel } from './rules';
import type { Session } from './types';

/**
 * Describes, in words, how a refetched session list differs from what was on screen,
 * so a live update can be announced (role="status") instead of silently changing rows.
 * Sessions that left the list are looked up over HTTP to say why (e.g. cancelled).
 * Returns an empty string when nothing visible changed.
 */
export async function describeChanges(before: Session[], after: Session[], timeZone?: string): Promise<string> {
  const when = new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  const name = (s: Session) => `${s.subject_name} on ${when.format(new Date(s.starts_at))}`;
  const old = new Map(before.map((s) => [s.id, s]));
  const now = new Map(after.map((s) => [s.id, s]));
  const lines: string[] = [];

  for (const session of after) {
    const previous = old.get(session.id);
    if (!previous) lines.push(`New: ${name(session)} (${statusLabel(session)}).`);
    else if (previous.status !== session.status) lines.push(`${name(session)}: now ${statusLabel(session)}.`);
  }
  for (const session of before) {
    if (now.has(session.id)) continue;
    try {
      const current = await sessionsApi.get(session.id);
      lines.push(`${name(session)}: now ${statusLabel(current)}.`);
    } catch {
      lines.push(`${name(session)} is no longer in this list.`);
    }
  }
  return lines.join(' ');
}
