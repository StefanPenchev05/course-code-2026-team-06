import { FlaggedList } from '../features/accountDiscipline';

/** /coordinator/flags (#56): accounts flagged for your faculty after repeated violations. */
export function CoordinatorFlagsPage() {
  return (
    <section className="page">
      <h1>Flagged accounts</h1>
      <p>
        An account is flagged for its home faculty after two violations in the same semester: confirmed no-shows or late
        cancellations. A flag never suspends anyone. Open an account to see its history and, if needed, suspend it.
      </p>
      <FlaggedList />
    </section>
  );
}
