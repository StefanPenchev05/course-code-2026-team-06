import { useEffect, useId, useState } from 'react';
import { ApiError, type Pagination } from '../../shared/api/client';
import { errorMessage, noLongerVisible } from '../../shared/api/errors';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, PageLoader } from '../../shared/ui';
import { monitoringApi } from './api';
import { Totals } from './Totals';
import { ActiveBadge } from './TutorDirectory';
import { TutorFeedback } from './TutorFeedback';
import { TutorSchedule } from './TutorSchedule';
import type { TutorDetail } from './types';

type Load =
  | { attempt: number; status: 'ready'; tutor: TutorDetail; offerings: Pagination }
  | { attempt: number; status: 'error'; error: unknown };

/** One tutor of the coordinator's faculty: totals, subjects, weekly schedule and feedback (UC12). */
export function TutorOverview({ id }: { id: string }) {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const subjectsHeading = useId();

  useEffect(() => {
    const controller = new AbortController();
    monitoringApi.tutor(id, controller.signal).then(
      (response) => setLoad({ attempt, status: 'ready', tutor: response.data, offerings: response.offerings_pagination }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ attempt, status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [id, attempt]);

  // Live refresh (#93): totals and subjects update in place. activity.changed names the
  // tutor; application and case events name a record, so any of those re-checks. If the
  // tutor left this faculty, the page drops their data.
  const refresh = () => {
    monitoringApi.tutor(id).then(
      (response) =>
        setLoad((previous) =>
          previous?.status === 'ready' ? { ...previous, tutor: response.data, offerings: response.offerings_pagination } : previous,
        ),
      (error: unknown) => {
        if (noLongerVisible(error)) setLoad({ attempt, status: 'error', error });
      },
    );
  };
  useLiveRefresh(['activity.changed'], refresh, { entityId: id });
  useLiveRefresh(['tutor_application.changed', 'coordination.changed'], refresh);

  const current = load?.attempt === attempt ? load : null;
  if (!current) {
    return (
      <>
        <h1>Tutor</h1>
        <PageLoader label="Loading tutor…" />
      </>
    );
  }
  if (current.status === 'error') {
    const missing = current.error instanceof ApiError && (current.error.status === 404 || current.error.status === 422);
    return (
      <>
        <h1>Tutor</h1>
        {missing ? (
          <Alert title="Tutor not found">This tutor is not in your faculty, or doesn’t exist.</Alert>
        ) : (
          <Alert
            action={
              <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
                Try again
              </Button>
            }
          >
            {errorMessage(current.error)}
          </Alert>
        )}
      </>
    );
  }

  const { tutor, offerings } = current;
  const since = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(tutor.approved_at));

  return (
    <>
      <div className="session__head">
        <h1>{tutor.name}</h1>
        <ActiveBadge active={tutor.active} />
      </div>
      <p className="muted">
        {tutor.program_name} · Tutor since {since}
      </p>
      {!tutor.active && (
        <p className="muted">
          Students can’t book this tutor right now, for example because the account is suspended or no subject is active. Past
          activity is still shown.
        </p>
      )}
      <Totals totals={tutor} />
      <p className="muted">Totals count sessions of your faculty that are completed; no-shows are left out.</p>

      <section aria-labelledby={subjectsHeading}>
        <h2 id={subjectsHeading}>Subjects</h2>
        {tutor.offerings.length === 0 ? (
          <p>This tutor has no approved subjects.</p>
        ) : (
          <ul className="card-list">
            {tutor.offerings.map((offering) => (
              <li key={offering.id} className="card">
                <div className="session__head">
                  <h3 className="card__title">{offering.subject_name}</h3>
                  {!offering.active && <span className="badge badge--inactive">Inactive</span>}
                </div>
                {offering.description && <p className="preserve-lines">{offering.description}</p>}
              </li>
            ))}
          </ul>
        )}
        {offerings.has_more && <p className="muted">Only the first {tutor.offerings.length} subjects are shown.</p>}
      </section>

      <TutorSchedule tutorId={tutor.id} />
      <TutorFeedback tutorId={tutor.id} />
    </>
  );
}
