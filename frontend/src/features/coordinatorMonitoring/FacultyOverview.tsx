import { useEffect, useState } from 'react';
import { errorMessage } from '../../shared/api/errors';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, Spinner } from '../../shared/ui';
import { monitoringApi } from './api';
import { MONITORING_EVENTS } from './rules';
import { Totals } from './Totals';
import type { FacultyStatistics } from './types';

type Load = { attempt: number; status: 'ready'; statistics: FacultyStatistics } | { attempt: number; status: 'error'; message: string };

/** Tutoring totals of the coordinator's faculty (FR16, UC12). */
export function FacultyOverview() {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    monitoringApi.statistics(controller.signal).then(
      (statistics) => setLoad({ attempt, status: 'ready', statistics }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ attempt, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [attempt]);

  // Live refresh (#93): new totals replace the shown ones in place; a failure keeps them.
  useLiveRefresh(MONITORING_EVENTS, () => {
    monitoringApi.statistics().then(
      (statistics) => setLoad((previous) => (previous?.status === 'ready' ? { ...previous, statistics } : previous)),
      () => undefined,
    );
  });

  const current = load?.attempt === attempt ? load : null;
  if (!current) return <Spinner label="Loading faculty totals…" />;
  if (current.status === 'error') {
    return (
      <Alert
        title="The faculty totals could not be loaded"
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

  const { statistics } = current;
  return (
    <>
      <Totals
        totals={statistics}
        before={[
          ['Approved tutors', statistics.approved_tutors],
          ['Active tutors', statistics.active_tutors],
        ]}
      />
      <p className="muted">
        Active tutors can be booked right now. Totals count sessions of your faculty that are completed; no-shows are left out.
      </p>
    </>
  );
}
