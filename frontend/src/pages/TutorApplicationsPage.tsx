import { StudentApplications } from '../features/tutorApplications';

/** /tutor-applications: verified students apply to tutor a subject and follow the decisions (#30). */
export function TutorApplicationsPage() {
  return (
    <section className="page">
      <h1>Become a tutor</h1>
      <p>
        Apply to tutor one subject at a time. You need a grade of at least 15/20 in that subject. The coordinator of
        your faculty reviews each application, and you get an email with the decision.
      </p>
      <StudentApplications />
    </section>
  );
}
