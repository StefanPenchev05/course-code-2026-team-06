import { useCallback, useId, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { Alert, Button, SelectField, Spinner } from '../../shared/ui';
import { disciplineApi } from './api';
import { actorText, CATEGORY_LABEL, formatDateTime, reference, VIOLATION_LABEL } from './rules';
import { AUDIT_CATEGORIES, type AuditCategory, type AuditEvent } from './types';

const EMPTY: Record<AuditCategory, string> = {
  SUSPENSION: 'No suspension decisions recorded by your faculty.',
  VIOLATION: 'No violations visible to your faculty.',
  FLAG: 'No flags recorded for your faculty.',
};

/** The account's history as your faculty may see it, one category at a time, newest first (NFR12). */
export function AccountHistory({ userId }: { userId: string }) {
  const [category, setCategory] = useState<AuditCategory>('VIOLATION');
  const heading = useId();

  return (
    <section aria-labelledby={heading}>
      <h2 id={heading}>History</h2>
      <p className="muted">Only events recorded for your faculty are shown. Names and evidence are not part of this history.</p>
      <div className="filters">
        <SelectField label="Show" value={category} onChange={(event) => setCategory(event.target.value as AuditCategory)}>
          {AUDIT_CATEGORIES.map((option) => (
            <option key={option} value={option}>
              {CATEGORY_LABEL[option]}
            </option>
          ))}
        </SelectField>
      </div>
      <HistoryList key={category} userId={userId} category={category} />
    </section>
  );
}

function HistoryList({ userId, category }: { userId: string; category: AuditCategory }) {
  const fetchPage = useCallback(
    (offset: number, signal?: AbortSignal) => disciplineApi.history(userId, category, offset, signal),
    [userId, category],
  );
  const { state, reload, loadMore, loadingMore, moreError } = usePagedList(fetchPage);

  if (state.status === 'loading') return <Spinner label="Loading history…" />;
  if (state.status === 'error') {
    return (
      <Alert
        action={
          <Button variant="secondary" onClick={reload}>
            Try again
          </Button>
        }
      >
        {errorMessage(state.error)}
      </Alert>
    );
  }
  if (state.items.length === 0) return <p>{EMPTY[category]}</p>;

  return (
    <>
      <ul className="card-list" aria-label={CATEGORY_LABEL[category]}>
        {state.items.map((event) => (
          <li key={event.id} className="card">
            <EventText event={event} />
          </li>
        ))}
      </ul>
      {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
      {state.hasMore && (
        <Button variant="secondary" loading={loadingMore} onClick={loadMore}>
          Show more
        </Button>
      )}
    </>
  );
}

function EventText({ event }: { event: AuditEvent }) {
  const meta = `${formatDateTime(event.recorded_at)} · ${actorText(event.actor_kind, event.actor_user_id)}`;

  if (event.category === 'SUSPENSION') {
    return (
      <>
        <h3 className="card__title">{event.is_suspended ? 'Suspended' : 'Reinstated'}</h3>
        <p className="card__meta">{meta}</p>
        {event.reason && <p className="preserve-lines">{event.reason}</p>}
      </>
    );
  }
  if (event.category === 'VIOLATION') {
    return (
      <>
        <h3 className="card__title">{(event.violation_kind && VIOLATION_LABEL[event.violation_kind]) ?? event.violation_kind ?? 'Violation'}</h3>
        <p className="card__meta">{meta}</p>
        <p>
          {event.occurred_at && `Happened ${formatDateTime(event.occurred_at)} · `}
          {event.session_id && `Session ${reference(event.session_id)} · `}
          {event.semester_id && `Semester ${reference(event.semester_id)}`}
        </p>
      </>
    );
  }
  return (
    <>
      <h3 className="card__title">Flagged{event.semester_id ? ` for semester ${reference(event.semester_id)}` : ''}</h3>
      <p className="card__meta">{meta}</p>
      {event.first_violation_id && event.second_violation_id && (
        <p>
          After violations {reference(event.first_violation_id)} and {reference(event.second_violation_id)}.
        </p>
      )}
    </>
  );
}
