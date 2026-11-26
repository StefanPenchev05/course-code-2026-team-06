import { VerificationQueue } from '../features/verifications';

/** /coordinator/verifications (#51): student registrations waiting for review, oldest first. */
export function CoordinatorVerificationsPage() {
  return (
    <section className="page">
      <h1>Student registrations</h1>
      <p>
        New students of your faculty can use Tutti only after you approve their registration. Open one to check it against the
        university’s records.
      </p>
      <VerificationQueue />
    </section>
  );
}
