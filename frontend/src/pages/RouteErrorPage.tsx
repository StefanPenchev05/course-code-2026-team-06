import { Link } from 'react-router';

/** Shown if a page crashes, instead of a blank screen. */
export function RouteErrorPage() {
  return (
    <section className="page" role="alert">
      <h1>Something went wrong</h1>
      <p>Please reload the page. If it keeps happening, tell the team what you were doing.</p>
      <p>
        <Link to="/">Go to the home page</Link>
      </p>
    </section>
  );
}
