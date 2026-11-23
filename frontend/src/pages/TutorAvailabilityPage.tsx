import { useState } from 'react';
import { OneOffForm } from '../features/availability/OneOffForm';
import { UpcomingWindows } from '../features/availability/UpcomingWindows';
import { WeeklyRules } from '../features/availability/WeeklyRules';

/** A tutor manages when students can book them (#31). */
export function TutorAvailabilityPage() {
  // Any change (add, remove, edit, restore) reloads every list on the page.
  const [reloadKey, setReloadKey] = useState(0);
  const changed = () => setReloadKey((n) => n + 1);

  return (
    <section className="page">
      <h1>My availability</h1>
      <p>Students can book you only during these times, for any subject you are approved for.</p>
      <UpcomingWindows reloadKey={reloadKey} onChanged={changed} />
      <OneOffForm onAdded={changed} />
      <WeeklyRules reloadKey={reloadKey} onChanged={changed} />
    </section>
  );
}
