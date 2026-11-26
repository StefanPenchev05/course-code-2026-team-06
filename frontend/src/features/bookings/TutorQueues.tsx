import { useCallback, useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, Spinner } from '../../shared/ui';
import { sessionsApi } from './api';
import { describeChanges } from './liveChanges';
import { TutorSessionCard } from './TutorSessionCard';
import type { Session } from './types';

type Queues = { requested: Session[]; accepted: Session[]; more: boolean };
type Load = ({ attempt: number; status: 'ok' } & Queues) | { attempt: number; status: 'error'; message: string };

const byStart = (a: Session, b: Session) => a.starts_at.localeCompare(b.starts_at) || a.id.localeCompare(b.id);

async function loadQueues(signal?: AbortSignal): Promise<Queues> {
  const [requested, accepted] = await Promise.all([sessionsApi.tutorQueue('REQUESTED', 0, signal), sessionsApi.tutorQueue('ACCEPTED', 0, signal)]);
  return { requested: requested.data, accepted: accepted.data, more: requested.pagination.has_more || accepted.pagination.has_more };
}

/** The tutor's requests waiting for an answer, and confirmed sessions to hold and later complete. */
export function TutorQueues() {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [announcement, setAnnouncement] = useState('');
  const shown = useRef<Load | null>(null);
  useEffect(() => {
    shown.current = load;
  }, [load]);

  useEffect(() => {
    const controller = new AbortController();
    loadQueues(controller.signal).then(
      (queues) => setLoad({ attempt, status: 'ok', ...queues }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ attempt, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [attempt]);

  /**
   * Live refresh (#91): new requests, student cancellations, expiries and the like appear
   * without a reload. Cards stay mounted, so a half-written answer is kept unless that
   * session itself can no longer be answered.
   */
  const refresh = useCallback(async () => {
    const before = shown.current;
    if (before?.status !== 'ok') return;
    try {
      const queues = await loadQueues();
      const after = shown.current;
      if (after?.status !== 'ok') return;
      setLoad({ ...after, ...queues });
      const text = await describeChanges([...before.requested, ...before.accepted], [...queues.requested, ...queues.accepted]);
      if (text) setAnnouncement(text);
    } catch {
      // Keep what is shown; the next event or reconnection tries again.
    }
  }, []);

  useLiveRefresh(['booking.changed'], () => void refresh());

  const current = load?.attempt === attempt ? load : null;
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

  function changed(updated: Session) {
    setLoad((previous) => {
      if (previous?.status !== 'ok') return previous;
      const requested = previous.requested.filter((s) => s.id !== updated.id);
      const others = previous.accepted.filter((s) => s.id !== updated.id);
      const accepted = updated.status === 'ACCEPTED' ? [...others, updated].sort(byStart) : others;
      return { ...previous, requested, accepted };
    });
    setNotice(
      updated.status === 'ACCEPTED'
        ? `Accepted. The student was notified and sees the ${updated.mode === 'ONLINE' ? 'meeting link' : 'room'}.`
        : updated.status === 'DECLINED'
          ? 'Declined. The student was notified with your reason.'
          : updated.status === 'COMPLETED'
            ? 'Marked as completed. The student can now rate the session.'
            : `The session is now ${updated.status.toLowerCase()}.`,
    );
  }

  const stale = () => void refresh();

  return (
    <>
      {notice && <Alert kind="success">{notice}</Alert>}
      {/* Changes made by students or the system, announced politely. */}
      <p className="live-announcement" role="status" aria-live="polite">
        {announcement}
      </p>
      <section aria-labelledby="requests-heading">
        <h2 id="requests-heading">Requests waiting for you ({current.requested.length})</h2>
        {current.requested.length === 0 ? (
          <p className="muted">No requests right now.</p>
        ) : (
          <ul className="card-list">
            {current.requested.map((session) => (
              <TutorSessionCard key={session.id} session={session} onChanged={changed} onStale={stale} />
            ))}
          </ul>
        )}
      </section>
      <section aria-labelledby="confirmed-heading">
        <h2 id="confirmed-heading">Confirmed sessions ({current.accepted.length})</h2>
        {current.accepted.length === 0 ? (
          <p className="muted">No confirmed sessions.</p>
        ) : (
          <ul className="card-list">
            {current.accepted.map((session) => (
              <TutorSessionCard key={session.id} session={session} onChanged={changed} onStale={stale} />
            ))}
          </ul>
        )}
      </section>
      {current.more && <p className="muted">Only the first 50 of each list are shown.</p>}
    </>
  );
}
