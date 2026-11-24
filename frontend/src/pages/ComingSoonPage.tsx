const ISSUES_URL = 'https://github.com/StefanPenchev05/course-code-2026-team-06/issues';

/** Placeholder for a screen that hasn't been built yet. */
export function ComingSoonPage({ title, issue }: { title: string; issue: number }) {
  return (
    <section className="page">
      <h1>{title}</h1>
      <p>
        This screen is planned in <a href={`${ISSUES_URL}/${issue}`}>issue #{issue}</a>.
      </p>
    </section>
  );
}
