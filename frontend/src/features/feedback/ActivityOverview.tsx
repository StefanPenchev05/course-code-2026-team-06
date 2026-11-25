import { useEffect, useId, useState } from 'react';
import { Link } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { Alert, Button, Spinner } from '../../shared/ui';
import { browserTimeZone, MODE_LABEL } from '../../shared/utils/datetime';
import { feedbackApi } from './api';
import { RatingStars } from './Rating';
import type { FeedbackEntry, TutorActivity, UpcomingSession } from './types';

type Load =
  | { attempt: number; status: 'ready'; activity: TutorActivity; feedback: FeedbackEntry[]; hasMoreFeedback: boolean }
  | { attempt: number; status: 'error'; message: string };

const isWebLink = (value: string) => /^https?:\/\//i.test(value);

/** A tutor's totals, next accepted sessions and the feedback students gave (FR14, UC10). */
export function ActivityOverview({ timeZone = browserTimeZone() }: { timeZone?: string }) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);
  const upcomingHeading = useId();
  const feedbackHeading = useId();

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([feedbackApi.activity(controller.signal), feedbackApi.received(0, controller.signal)]).then(
      ([activity, feedback]) =>
        setLoad({ attempt, status: 'ready', activity, feedback: feedback.data, hasMoreFeedback: feedback.pagination.has_more }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ attempt, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [attempt]);

  const current = load?.attempt === attempt ? load : null;
  if (!current) return <Spinner label="Loading your activity…" />;
  if (current.status === 'error') {
    return (
      <Alert
        title="Your activity could not be loaded"
        action={
          <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Button>
        }
      >
        {current.message}
      </Alert>
    );
  }

  const { activity, feedback } = current;
  const upcoming = activity.upcoming_sessions;
  const hours = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 });
  const day = new Intl.DateTimeFormat(undefined, { timeZone, day: 'numeric', month: 'long', year: 'numeric' });

  async function showMoreFeedback() {
    if (current?.status !== 'ready') return;
    setLoadingMore(true);
    setMoreError(null);
    try {
      const page = await feedbackApi.received(current.feedback.length);
      // New feedback shifts the pages (newest first), so skip entries already shown.
      const shown = new Set(current.feedback.map((entry) => entry.session_id));
      setLoad({
        ...current,
        feedback: [...current.feedback, ...page.data.filter((entry) => !shown.has(entry.session_id))],
        hasMoreFeedback: page.pagination.has_more,
      });
    } catch (error) {
      setMoreError(errorMessage(error));
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <>
      <dl className="stats">
        <div className="stats__item">
          <dt>Completed sessions</dt>
          <dd>{activity.completed_sessions}</dd>
        </div>
        <div className="stats__item">
          <dt>Hours taught</dt>
          <dd>{hours.format(activity.hours_taught)}</dd>
        </div>
        <div className="stats__item">
          <dt>Average rating</dt>
          {activity.average_rating === null ? (
            <dd className="stats__empty">No ratings yet</dd>
          ) : (
            <dd>{activity.average_rating.toFixed(1)} / 5</dd>
          )}
        </div>
        <div className="stats__item">
          <dt>Ratings received</dt>
          <dd>{activity.rating_count}</dd>
        </div>
      </dl>
      <p className="muted">Only sessions marked as completed count. Sessions confirmed as no-shows are left out.</p>

      <section aria-labelledby={upcomingHeading}>
        <h2 id={upcomingHeading}>Upcoming sessions</h2>
        {upcoming.length === 0 ? (
          <p>
            No confirmed sessions are coming up. Students can book you during your <Link to="/tutor/availability">free times</Link>.
          </p>
        ) : (
          <ul className="card-list">
            {upcoming.map((session) => (
              <UpcomingCard key={session.id} session={session} timeZone={timeZone} />
            ))}
          </ul>
        )}
        {activity.pagination.has_more && (
          <p className="muted">
            Only the next {upcoming.length} are shown. All confirmed sessions are on the <Link to="/tutor">Teaching</Link> page.
          </p>
        )}
      </section>

      <section aria-labelledby={feedbackHeading}>
        <h2 id={feedbackHeading}>Feedback from students</h2>
        {feedback.length === 0 ? (
          <p>No feedback yet. Students can rate a session once you have marked it as completed.</p>
        ) : (
          <ul className="card-list">
            {feedback.map((entry) => (
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
        )}
        {moreError && <Alert>{moreError}</Alert>}
        {current.hasMoreFeedback && (
          <Button variant="secondary" onClick={() => void showMoreFeedback()} loading={loadingMore}>
            Show more feedback
          </Button>
        )}
      </section>
    </>
  );
}

function UpcomingCard({ session, timeZone }: { session: UpcomingSession; timeZone: string }) {
  const when = new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  const until = new Intl.DateTimeFormat(undefined, { timeZone, hour: '2-digit', minute: '2-digit' });

  return (
    <li className="card">
      <h3 className="card__title">{session.subject_name}</h3>
      <p className="card__meta">
        {when.format(new Date(session.starts_at))}–{until.format(new Date(session.ends_at))} · {MODE_LABEL[session.mode]}
      </p>
      {session.meeting_link && (
        <p>
          Meeting link:{' '}
          {isWebLink(session.meeting_link) ? (
            <a href={session.meeting_link} target="_blank" rel="noreferrer noopener">
              {session.meeting_link}
            </a>
          ) : (
            session.meeting_link
          )}
        </p>
      )}
      {session.meeting_room && <p>Room: {session.meeting_room}</p>}
    </li>
  );
}
