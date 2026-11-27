import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useAuth } from '../features/auth/AuthContext';
import { gatewayUrl, RealtimeClient, RealtimeContext, type ConnectionStatus, type RealtimeState } from '../shared/realtime';

/**
 * Owns the app's single realtime connection (issue #90).
 * - Connects only while a user is logged in, once per browser tab; stops on logout.
 * - Restarts when the account's access changes (roles, verification, program), since the
 *   server closes the socket then and the new connection must use the new access.
 * - After every (re)connection re-checks the session, because `account.access_changed`
 *   may have been missed; on that event it re-checks right away.
 * - If a connection is refused, re-checks the session: when the user is logged out the
 *   connection stops instead of retrying forever.
 */
export function RealtimeProvider({ children }: { children: ReactNode }) {
  const { user, refresh } = useAuth();
  const [status, setStatus] = useState<ConnectionStatus>('idle');
  const [connectionNumber, setConnectionNumber] = useState(0);

  // Everything that decides what the server may send this user.
  const accessKey = user
    ? [user.id, [...user.roles].sort().join('+'), user.verified_student, user.verification_status ?? '', user.program_id ?? '', user.coordinator_faculty_id ?? ''].join('|')
    : null;

  // Creating a client opens nothing; only start() does. One client per access key.
  const client = useMemo(
    () =>
      accessKey
        ? new RealtimeClient({
            url: gatewayUrl(),
            onStatus: setStatus,
            onConnected: (number) => {
              setConnectionNumber(number);
              void refresh();
            },
            onRejected: () => void refresh(),
          })
        : null,
    [accessKey, refresh],
  );

  useEffect(() => {
    if (!client) return;
    const offAccess = client.subscribe('account.access_changed', () => void refresh());
    // Start on the next tick: React's development StrictMode mounts, unmounts and mounts again
    // right away; the first cleanup then cancels this before any socket exists.
    const timer = setTimeout(() => client.start(), 0);
    return () => {
      clearTimeout(timer);
      offAccess();
      client.stop();
      setStatus('idle');
      setConnectionNumber(0);
    };
  }, [client, refresh]);

  // Exposes the connection state as <html data-live="open|reconnecting|…"> so browser journeys
  // (tests/e2e, #94) can wait until the live connection is open instead of sleeping.
  useEffect(() => {
    document.documentElement.dataset.live = status;
  }, [status]);

  const value = useMemo<RealtimeState>(() => ({ client, status, connectionNumber }), [client, status, connectionNumber]);
  return <RealtimeContext.Provider value={value}>{children}</RealtimeContext.Provider>;
}
