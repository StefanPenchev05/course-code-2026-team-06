import { useEffect, useId, useState, type FormEvent } from 'react';
import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, Spinner, TextArea } from '../../shared/ui';
import { browserTimeZone, groupByDay, MODE_LABEL, timeRange } from '../../shared/utils/datetime';
import { monitoringApi } from './api';
import { addDays, cancellationError, isCancellable, reasonProblem, startOfWeek, STATUS_LABEL } from './rules';
import type { ScheduleEntry } from './types';

type Load =
  | { key: string; status: 'ready'; entries: ScheduleEntry[]; hasMore: boolean }
  | { key: string; status: 'error'; message: string };

type Notice = { kind: 'success' | 'warning'; text: string };

interface TutorScheduleProps {
  tutorId: string;
  /** Injected for tests; defaults to now. */
  now?: Date;
}

/**
 * The tutor's week: published free times and sessions in every status, in the browser's
 * time zone. Requested and confirmed sessions can be cancelled for policy reasons (FR19, UC04).
 */
export function TutorSchedule({ tutorId, now }: TutorScheduleProps) {
  const [thisWeek] = useState(() => startOfWeek(now ?? new Date()));
  const [week, setWeek] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [cancelling, setCancelling] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const heading = useId();
  const timeZone = browserTimeZone();

  const from = addDays(thisWeek, week * 7);
  const key = `${tutorId}#${week}#${attempt}`;

  useEffect(() => {
    const controller = new AbortController();
    const weekStart = addDays(thisWeek, week * 7);
    monitoringApi.schedule(tutorId, weekStart, addDays(weekStart, 7), controller.signal).then(
      (page) => setLoad({ key, status: 'ready', entries: page.data, hasMore: page.pagination.has_more }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ key, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [tutorId, thisWeek, week, key]);

  const current = load?.key === key ? load : null;
  const day = new Intl.DateTimeFormat(undefined, { timeZone, day: 'numeric', month: 'long' });

  function changeWeek(next: number) {
    setWeek(next);
    setCancelling(null);
    setNotice(null);
  }

  function cancelled(id: string) {
    setCancelling(null);
    setNotice({ kind: 'success', text: 'The session was cancelled. Both participants are notified and can read your reason.' });
    setLoad((previous) =>
      previous?.status === 'ready'
        ? { ...previous, entries: previous.entries.map((entry) => (entry.id === id && entry.kind === 'SESSION' ? { ...entry, status: 'CANCELLED' } : entry)) }
        : previous,
    );
  }

  function changedMeanwhile(message: string) {
    setCancelling(null);
    setNotice({ kind: 'warning', text: `${message} The schedule was refreshed.` });
    setAttempt((n) => n + 1);
  }

  return (
    <section aria-labelledby={heading}>
      <h2 id={heading}>Schedule</h2>
      <p className="muted">
        Week of {day.format(from)} – {day.format(addDays(from, 6))}, shown in your time zone ({timeZone}).
      </p>
      <div className="button-row schedule-nav">
        <Button variant="secondary" onClick={() => changeWeek(week - 1)}>
          Previous week
        </Button>
        {week !== 0 && (
          <Button variant="secondary" onClick={() => changeWeek(0)}>
            This week
          </Button>
        )}
        <Button variant="secondary" onClick={() => changeWeek(week + 1)}>
          Next week
        </Button>
      </div>

      {notice && <Alert kind={notice.kind}>{notice.text}</Alert>}
      {!current && <Spinner label="Loading the schedule…" />}
      {current?.status === 'error' && (
        <Alert
          action={
            <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
              Try again
            </Button>
          }
        >
          {current.message}
        </Alert>
      )}
      {current?.status === 'ready' && current.entries.length === 0 && <p>No free times or sessions this week.</p>}
      {current?.status === 'ready' && current.entries.length > 0 && (
        <div className="day-list">
          {groupByDay(current.entries, timeZone).map((group) => (
            <div key={group.key}>
              <h3 className="day__title">{group.label}</h3>
              <ul className="schedule-list">
                {group.items.map((entry) =>
                  entry.kind === 'AVAILABILITY' ? (
                    <li key={`free-${entry.id}-${entry.starts_at}`} className="schedule-entry schedule-entry--free">
                      <span className="slot__time">{timeRange(entry, timeZone)}</span> Free time · {MODE_LABEL[entry.mode] ?? entry.mode}
                      {entry.rule_id ? ' · weekly' : ''}
                    </li>
                  ) : (
                    <li key={`session-${entry.id}`} className="schedule-entry">
                      <div className="session__head">
                        <span>
                          <span className="slot__time">{timeRange(entry, timeZone)}</span> {entry.subject_name}
                        </span>
                        <span className={`badge badge--${(entry.status ?? '').toLowerCase()}`}>
                          {entry.status ? (STATUS_LABEL[entry.status] ?? entry.status) : 'Session'}
                        </span>
                      </div>
                      <p className="card__meta">Session · {MODE_LABEL[entry.mode] ?? entry.mode}</p>
                      {isCancellable(entry.status) &&
                        (cancelling === entry.id ? (
                          <PolicyCancelForm
                            entry={entry}
                            onClose={() => setCancelling(null)}
                            onCancelled={() => cancelled(entry.id)}
                            onConflict={changedMeanwhile}
                          />
                        ) : (
                          <Button
                            variant="link"
                            onClick={() => {
                              setNotice(null);
                              setCancelling(entry.id);
                            }}
                            aria-label={`Cancel the ${entry.subject_name ?? ''} session on ${group.label}, ${timeRange(entry, timeZone)}`}
                          >
                            Cancel session
                          </Button>
                        ))}
                    </li>
                  ),
                )}
              </ul>
            </div>
          ))}
        </div>
      )}
      {current?.status === 'ready' && current.hasMore && <p className="muted">Only the first {current.entries.length} entries of this week are shown.</p>}
    </section>
  );
}

interface PolicyCancelFormProps {
  entry: ScheduleEntry;
  onClose: () => void;
  onCancelled: () => void;
  onConflict: (message: string) => void;
}

function PolicyCancelForm({ entry, onClose, onCancelled, onConflict }: PolicyCancelFormProps) {
  const [reason, setReason] = useState('');
  const [reasonError, setReasonError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const problem = reasonProblem(reason);
    setReasonError(problem);
    setFormError(null);
    if (problem) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      await monitoringApi.cancelSession(entry.id, reason.trim());
      onCancelled();
    } catch (error) {
      setSubmitting(false);
      if (error instanceof ApiError && error.status === 409) {
        onConflict(cancellationError(error));
        return;
      }
      setFormError(cancellationError(error));
      signalFailure();
    }
  }

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form cancel-form" aria-label="Cancel for policy reasons">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      <Alert kind="warning" title="Cancel this session for policy reasons?">
        The student and the tutor are both notified and the time becomes free again. This can’t be undone. It is not recorded as a
        late cancellation for either of them.
      </Alert>
      <TextArea
        label="Reason for cancelling"
        name="reason"
        value={reason}
        onChange={(event) => {
          setReason(event.target.value);
          setReasonError(undefined);
        }}
        error={reasonError}
        hint="Both participants can read this reason."
      />
      <div className="button-row">
        <Button type="submit" variant="secondary" loading={submitting}>
          Cancel the session
        </Button>
        <Button variant="link" onClick={onClose} disabled={submitting}>
          Keep it
        </Button>
      </div>
    </form>
  );
}
