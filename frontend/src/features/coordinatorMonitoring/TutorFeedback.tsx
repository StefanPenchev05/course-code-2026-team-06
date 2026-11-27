import { useCallback, useId, type ReactNode } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { usePagedList } from '../../shared/hooks/usePagedList';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, LiveAnnouncement, Spinner } from '../../shared/ui';
import { RatingStars } from '../feedback';
import { monitoringApi } from './api';
import type { FeedbackEntry } from './types';

/** One feedback entry per session. */
const feedbackKey = (entry: FeedbackEntry) => entry.session_id;

/** Ratings and private comments on the tutor's completed sessions of this faculty (FR16). */
export function TutorFeedback({ tutorId }: { tutorId: string }) {
  const heading = useId();
  const fetchFeedback = useCallback((offset: number, signal?: AbortSignal) => monitoringApi.feedback(tutorId, offset, signal), [tutorId]);
  const { state, reload, refresh, change, loadMore, loadingMore, moreError } = usePagedList(fetchFeedback, feedbackKey);
  // New ratings and corrected sessions update in place (#93).
  useLiveRefresh(['activity.changed'], refresh, { entityId: tutorId });
  const day = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'long', year: 'numeric' });

  let content: ReactNode;
  if (state.status === 'loading') {
    content = <Spinner label="Loading feedback…" />;
  } else if (state.status === 'error') {
    content = (
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
  } else if (state.items.length === 0) {
    content = <p>No feedback on this tutor’s sessions in your faculty yet.</p>;
  } else {
    content = (
      <>
        <ul className="card-list">
          {state.items.map((entry) => (
            <li key={entry.session_id} className="card">
              <div className="session__head">
                <h3 className="card__title">{entry.subject_name}</h3>
                <RatingStars rating={entry.rating} />
              </div>
              <p className="card__meta">Session on {day.format(new Date(entry.starts_at))}</p>
              {entry.comment ? <p className="preserve-lines feedback__comment">{entry.comment}</p> : <p className="muted">No comment.</p>}
            </li>
          ))}
        </ul>
        {moreError !== null && <Alert>{errorMessage(moreError)}</Alert>}
        {state.hasMore && (
          <Button variant="secondary" loading={loadingMore} onClick={loadMore}>
            Show more feedback
          </Button>
        )}
      </>
    );
  }

  return (
    <section aria-labelledby={heading}>
      <h2 id={heading}>Feedback from students</h2>
      <p className="muted">Comments are private: only the tutor and the coordinators of your faculty can read them.</p>
      <LiveAnnouncement change={change} noun={['rating', 'ratings']} />
      {content}
    </section>
  );
}
