import { TutorDirectory } from '../features/coordinatorMonitoring';

/** /coordinator/tutors (#35): approved tutors of the coordinator's faculty, filtered in the URL. */
export function CoordinatorTutorsPage() {
  return (
    <section className="page">
      <h1>Tutors</h1>
      <p>Approved tutors whose study program is in your faculty. Open a tutor to see their schedule and feedback.</p>
      <TutorDirectory />
    </section>
  );
}
