import { useEffect, useRef } from 'react';
import { api } from '../api/client';
import { useRealtime } from './context';

/** Leases last ten minutes on the server; renew halfway (docs/realtime-delivery.md). */
const RENEW_MS = 5 * 60 * 1000;
/** The server allows at most 32 watched tutors per user. */
export const MAX_WATCHED_TUTORS = 32;

export function registerAvailabilityWatch(tutorId: string, signal?: AbortSignal) {
  return api.post<{ expires_at: string }>('/realtime/availability-watches', { tutor_id: tutorId }, { signal });
}

/**
 * Asks the server to send `availability.changed` for these tutors to this user while the view
 * is open: registers on mount and after every reconnection, renews every five minutes, and
 * simply stops renewing on unmount (the lease then expires; one tab can't cancel another's).
 * A refused watch (tutor no longer visible) is ignored: the view's own HTTP data shows that.
 * `onRegistered` runs after a re-registration that followed a reconnection, so the view can
 * refetch once more and cover changes that raced the registration.
 */
export function useAvailabilityWatch(tutorIds: readonly string[], onRegistered?: () => void) {
  const { client, connectionNumber } = useRealtime();
  const callback = useRef(onRegistered);
  const firstConnection = useRef(connectionNumber);
  useEffect(() => {
    callback.current = onRegistered;
  });

  const key = [...new Set(tutorIds)].filter(Boolean).slice(0, MAX_WATCHED_TUTORS).sort().join(',');

  useEffect(() => {
    if (!client || !key) return;
    const ids = key.split(',');
    const controller = new AbortController();
    const register = () =>
      Promise.all(ids.map((id) => registerAvailabilityWatch(id, controller.signal).catch(() => undefined))).then(() => {
        if (!controller.signal.aborted && connectionNumber > firstConnection.current) callback.current?.();
      });
    void register();
    const timer = setInterval(() => void register(), RENEW_MS);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [client, key, connectionNumber]);
}
