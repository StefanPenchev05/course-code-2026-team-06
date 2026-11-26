import { useEffect, useState } from 'react';
import { useRealtime } from '../shared/realtime';

const GRACE_MS = 5000;

/**
 * A quiet notice when live updates have been down for a few seconds. Short blips
 * (a reconnect within the grace period) show nothing, to avoid flicker.
 */
export function RealtimeNotice() {
  const { client, status } = useRealtime();
  const down = client !== null && (status === 'reconnecting' || status === 'connecting');
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (!down) return;
    const timer = setTimeout(() => setVisible(true), GRACE_MS);
    return () => {
      clearTimeout(timer);
      setVisible(false);
    };
  }, [down]);

  if (!down || !visible) return null;
  return (
    <p className="realtime-notice" role="status">
      Live updates are paused while we reconnect. What you see may be out of date; it refreshes automatically.
    </p>
  );
}
