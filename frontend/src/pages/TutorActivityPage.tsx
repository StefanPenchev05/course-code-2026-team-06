import { ActivityOverview } from '../features/feedback';

/** My activity (#34): teaching totals, upcoming confirmed sessions and feedback from students. */
export function TutorActivityPage() {
  return (
    <section className="page">
      <h1>My activity</h1>
      <p>Your teaching totals, your next confirmed sessions and what students said about their sessions with you.</p>
      <ActivityOverview />
    </section>
  );
}
