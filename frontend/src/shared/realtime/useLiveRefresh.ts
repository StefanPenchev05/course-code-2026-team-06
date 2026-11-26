import { useEffect, useRef } from 'react';
import { useRealtime } from './context';
import type { EventType } from './protocol';

interface LiveRefreshOptions {
  /** Only react to events about this entity (e.g. one tutor); events for others are ignored. */
  entityId?: string;
  /** Wait this long after the last trigger before reloading, so a burst causes one reload. */
  debounceMs?: number;
}

/**
 * Re-runs `reload` (an HTTP refetch) when one of `types` arrives, and after every
 * reconnection, because events sent while disconnected are not replayed. Unknown
 * event types also trigger a reload, as the contract asks. The event itself is
 * never used as data. Subscriptions are removed when the component unmounts.
 */
export function useLiveRefresh(types: readonly EventType[], reload: () => void, { entityId, debounceMs = 300 }: LiveRefreshOptions = {}) {
  const { client, connectionNumber } = useRealtime();
  const reloadRef = useRef(reload);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const firstConnection = useRef(connectionNumber);

  useEffect(() => {
    reloadRef.current = reload;
  });

  // Stable string so the subscription only changes when the list of types changes.
  const typeKey = [...types].sort().join(',');

  useEffect(() => {
    if (!client) return;
    const schedule = () => {
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        timer.current = null;
        reloadRef.current();
      }, debounceMs);
    };
    const offs = typeKey
      .split(',')
      .filter(Boolean)
      .map((type) =>
        client.subscribe(type as EventType, (event) => {
          if (!entityId || !('entityId' in event) || !event.entityId || event.entityId === entityId) schedule();
        }),
      );
    const offUnknown = client.subscribe('*', (event) => {
      if (event.type === 'unknown') schedule();
    });
    return () => {
      offs.forEach((off) => off());
      offUnknown();
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = null;
    };
  }, [client, typeKey, entityId, debounceMs]);

  // After a reconnection (not the connection that existed when the view mounted), refetch.
  useEffect(() => {
    if (connectionNumber > firstConnection.current) reloadRef.current();
  }, [connectionNumber]);
}
