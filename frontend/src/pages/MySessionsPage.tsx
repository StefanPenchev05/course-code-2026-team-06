import { useSearchParams } from 'react-router';
import { SessionList, type SessionTab } from '../features/bookings/SessionList';

/** My sessions (#32): sessions where I'm the student or the tutor, with status, role and cancellation. */
export function MySessionsPage() {
  const [params, setParams] = useSearchParams();
  const tab: SessionTab = params.get('show') === 'all' ? 'all' : 'active';

  return (
    <section className="page">
      <h1>My sessions</h1>
      <div className="tabs" role="group" aria-label="Which sessions">
        <button type="button" className="tabs__tab" aria-pressed={tab === 'active'} onClick={() => setParams({}, { replace: true })}>
          Requested and confirmed
        </button>
        <button type="button" className="tabs__tab" aria-pressed={tab === 'all'} onClick={() => setParams({ show: 'all' }, { replace: true })}>
          All sessions
        </button>
      </div>
      <SessionList key={tab} tab={tab} />
    </section>
  );
}
