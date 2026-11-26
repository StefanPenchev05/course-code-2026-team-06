import { useState } from 'react';
import { useAuth } from '../features/auth/AuthContext';
import { OneOffForm } from '../features/availability/OneOffForm';
import { UpcomingWindows } from '../features/availability/UpcomingWindows';
import { WeeklyRules } from '../features/availability/WeeklyRules';
import { useLiveRefresh } from '../shared/realtime';

/** A tutor manages when students can book them (#31). */
export function TutorAvailabilityPage() {
  // Any change (add, remove, edit, restore) reloads every list on the page.
  const [reloadKey, setReloadKey] = useState(0);
  const changed = () => setReloadKey((n) => n + 1);
  // Live (#92): bookings, cancellations and changes from another tab reach this page as
  // availability.changed for the tutor's own ID. A separate counter, so that the echo of the
  // tutor's own change doesn't hide the confirmation they just got.
  const { user } = useAuth();
  const [liveKey, setLiveKey] = useState(0);
  useLiveRefresh(['availability.changed'], () => setLiveKey((n) => n + 1), { entityId: user?.id });

  return (
    <section className="page">
      <h1>My availability</h1>
      <p>Students can book you only during these times, for any subject you are approved for.</p>
      <UpcomingWindows reloadKey={reloadKey} liveKey={liveKey} onChanged={changed} />
      <OneOffForm onAdded={changed} />
      <WeeklyRules reloadKey={reloadKey} liveKey={liveKey} onChanged={changed} />
    </section>
  );
}
