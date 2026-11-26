import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, Spinner } from '../../shared/ui';
import { sessionsApi } from './api';
import { describeChanges } from './liveChanges';
import { SessionCard } from './SessionCard';
import type { Session } from './types';

export type SessionTab = 'active' | 'all';

type Load = { key: string; status: 'ok'; sessions: Session[]; hasMore: boolean } | { key: string; status: 'error'; message: string };

const byStart = (a: Session, b: Session) => a.starts_at.localeCompare(b.starts_at) || a.id.localeCompare(b.id);
const PAGE = 50;

/** "All" pages until at least `atLeast` sessions are loaded, so a refresh keeps "Show more" results. */
async function loadAll(atLeast: number, signal?: AbortSignal): Promise<{ sessions: Session[]; hasMore: boolean }> {
  const sessions: Session[] = [];
  for (;;) {
    const page = await sessionsApi.mine(undefined, sessions.length, signal);
    sessions.push(...page.data);
    if (!page.pagination.has_more || sessions.length >= Math.max(atLeast, PAGE)) return { sessions, hasMore: page.pagination.has_more };
  }
}

/** "Active" = waiting for the tutor or confirmed (both reserve the time). "All" = every session, oldest first. */
async function loadTab(tab: SessionTab, atLeast = 0, signal?: AbortSignal): Promise<{ sessions: Session[]; hasMore: boolean }> {
  if (tab === 'all') return loadAll(atLeast, signal);
  const [requested, accepted] = await Promise.all([sessionsApi.mine('REQUESTED', 0, signal), sessionsApi.mine('ACCEPTED', 0, signal)]);
  return {
    sessions: [...requested.data, ...accepted.data].sort(byStart),
    hasMore: requested.pagination.has_more || accepted.pagination.has_more,
  };
}

export function SessionList({ tab }: { tab: SessionTab }) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [announcement, setAnnouncement] = useState('');
  const key = `${tab}#${attempt}`;
  // What is on screen, for comparing after a background refresh.
  const shown = useRef<Load | null>(null);
  useEffect(() => {
    shown.current = load;
  }, [load]);

  useEffect(() => {
    const controller = new AbortController();
    loadTab(tab, 0, controller.signal).then(
      (result) => setLoad({ key, status: 'ok', ...result }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [tab, key]);

  /**
   * Live refresh (#91): refetch in the background and swap the data in place. Cards stay
   * mounted, so open forms, typed text and focus survive; nothing shows a spinner.
   */
  const refresh = useCallback(async () => {
    const before = shown.current;
    if (before?.status !== 'ok') return;
    try {
      const result = await loadTab(tab, before.sessions.length);
      const after = shown.current;
      if (after?.status !== 'ok' || after.key !== before.key) return; // the tab changed meanwhile
      setLoad({ ...after, ...result });
      const text = await describeChanges(before.sessions, result.sessions);
      if (text) setAnnouncement(text);
    } catch {
      // Keep what is shown; the next event or reconnection tries again.
    }
  }, [tab]);

  useLiveRefresh(['booking.changed'], () => void refresh());

  const current = load?.key === key ? load : null;
  if (!current) return <Spinner label="Loading your sessions…" />;
  if (current.status === 'error') {
    return (
      <Alert
        action={
          <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Button>
        }
      >
        {current.message}
      </Alert>
    );
  }

  function replace(updated: Session) {
    setLoad((previous) =>
      previous?.status === 'ok'
        ? {
            ...previous,
            // A cancelled session leaves the "Active" list; in "All" it stays with its new status.
            sessions:
              tab === 'active' && !['REQUESTED', 'ACCEPTED'].includes(updated.status)
                ? previous.sessions.filter((s) => s.id !== updated.id)
                : previous.sessions.map((s) => (s.id === updated.id ? updated : s)),
          }
        : previous,
    );
  }

  async function showMore() {
    if (current?.status !== 'ok') return;
    setLoadingMore(true);
    try {
      const page = await sessionsApi.mine(undefined, current.sessions.length);
      setLoad({ ...current, sessions: [...current.sessions, ...page.data], hasMore: page.pagination.has_more });
    } catch (error) {
      setLoad({ key, status: 'error', message: errorMessage(error) });
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <>
      {/* Live changes made by the other person or the system, announced politely. */}
      <p className="live-announcement" role="status" aria-live="polite">
        {announcement}
      </p>
      {current.sessions.length === 0 ? (
        tab === 'active' ? (
          <p>
            You have no requested or confirmed sessions. <Link to="/tutors">Find a tutor</Link> to book one.
          </p>
        ) : (
          <p>You haven’t had any sessions yet.</p>
        )
      ) : (
        <ul className="card-list">
          {current.sessions.map((session) => (
            <SessionCard key={session.id} session={session} onChanged={replace} onStale={() => void refresh()} />
          ))}
        </ul>
      )}
      {current.hasMore && tab === 'all' && (
        <Button variant="secondary" onClick={() => void showMore()} loading={loadingMore}>
          Show more sessions
        </Button>
      )}
      {current.hasMore && tab === 'active' && <p className="muted">Only the first 50 of each kind are shown.</p>}
    </>
  );
}
