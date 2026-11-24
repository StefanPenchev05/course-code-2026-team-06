import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, Spinner } from '../../shared/ui';
import { sessionsApi } from './api';
import { SessionCard } from './SessionCard';
import type { Session } from './types';

export type SessionTab = 'active' | 'all';

type Load = { key: string; status: 'ok'; sessions: Session[]; hasMore: boolean } | { key: string; status: 'error'; message: string };

const byStart = (a: Session, b: Session) => a.starts_at.localeCompare(b.starts_at) || a.id.localeCompare(b.id);

/** "Active" = waiting for the tutor or confirmed (both reserve the time). "All" = every session, oldest first. */
async function loadTab(tab: SessionTab, signal: AbortSignal): Promise<{ sessions: Session[]; hasMore: boolean }> {
  if (tab === 'all') {
    const page = await sessionsApi.mine(undefined, 0, signal);
    return { sessions: page.data, hasMore: page.pagination.has_more };
  }
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
  const key = `${tab}#${attempt}`;

  useEffect(() => {
    const controller = new AbortController();
    loadTab(tab, controller.signal).then(
      (result) => setLoad({ key, status: 'ok', ...result }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [tab, key]);

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

  if (current.sessions.length === 0) {
    return tab === 'active' ? (
      <p>
        You have no requested or confirmed sessions. <Link to="/tutors">Find a tutor</Link> to book one.
      </p>
    ) : (
      <p>You haven’t had any sessions yet.</p>
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
      <ul className="card-list">
        {current.sessions.map((session) => (
          <SessionCard key={session.id} session={session} onChanged={replace} />
        ))}
      </ul>
      {current.hasMore && tab === 'all' && (
        <Button variant="secondary" onClick={() => void showMore()} loading={loadingMore}>
          Show more sessions
        </Button>
      )}
      {current.hasMore && tab === 'active' && <p className="muted">Only the first 50 of each kind are shown.</p>}
    </>
  );
}
