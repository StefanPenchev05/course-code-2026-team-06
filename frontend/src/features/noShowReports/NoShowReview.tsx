import { useEffect, useState, type FormEvent } from 'react';
import { noLongerVisible } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, PageLoader, TextArea } from '../../shared/ui';
import { noShowApi } from './api';
import { NoShowStatusBadge } from './NoShowStatusBadge';
import { decisionError, formatDateTime, isConflict, loadError, noteProblem, reference } from './rules';
import type { NoShowReport } from './types';

type Load = { version: number; status: 'ready'; report: NoShowReport } | { version: number; status: 'error'; error: unknown };

/**
 * One no-show allegation for a session taught in the coordinator's faculty, with the decision
 * (FR17, UC13). Render it with key={id}.
 */
export function NoShowReview({ id }: { id: string }) {
  const [version, setVersion] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [conflict, setConflict] = useState<string | null>(null);
  const [decidedElsewhere, setDecidedElsewhere] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    noShowApi.detail(id, controller.signal).then(
      (report) => setLoad({ version, status: 'ready', report }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ version, status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [id, version]);

  // Live refresh (#93): coordination.changed carries the report's ID. A decision made elsewhere
  // shows up in place; a note being typed stays while the report is pending. If the report is
  // no longer visible to this coordinator, its content is dropped.
  useLiveRefresh(
    ['coordination.changed'],
    () => {
      const wasPending = load?.status === 'ready' && load.report.status === 'PENDING';
      noShowApi.detail(id).then(
        (report) => {
          setLoad((previous) => (previous?.status === 'ready' ? { ...previous, report } : previous));
          if (wasPending && report.status !== 'PENDING') setDecidedElsewhere(true);
        },
        (error: unknown) => {
          if (noLongerVisible(error)) setLoad({ version, status: 'error', error });
        },
      );
    },
    { entityId: id },
  );

  // After an error, "Try again" shows the loader; after a conflict the last state stays until the new one arrives.
  if (!load || (load.status === 'error' && load.version !== version)) {
    return (
      <>
        <h1>No-show report</h1>
        <PageLoader label="Loading report…" />
      </>
    );
  }
  if (load.status === 'error') {
    return (
      <>
        <h1>No-show report</h1>
        <Alert
          action={
            <Button variant="secondary" onClick={() => setVersion((n) => n + 1)}>
              Try again
            </Button>
          }
        >
          {loadError(load.error)}
        </Alert>
      </>
    );
  }

  const { report } = load;

  return (
    <>
      <h1>No-show report {reference(report.id)}</h1>
      {done && <Alert kind="success">{done}</Alert>}
      {conflict && <Alert kind="warning">{conflict}</Alert>}
      {decidedElsewhere && !done && !conflict && (
        <Alert kind="info" title="Decided in the meantime">
          This report was decided elsewhere while you had it open, for example in another tab. The page now shows its
          current state.
        </Alert>
      )}

      <dl className="details">
        <dt>Status</dt>
        <dd>
          <NoShowStatusBadge status={report.status} />
        </dd>
        <dt>Session</dt>
        <dd>Ref. {reference(report.session_id)}</dd>
        <dt>Reported by</dt>
        <dd>Participant ref. {reference(report.reporter_user_id)}</dd>
        <dt>Said to be absent</dt>
        <dd>Participant ref. {reference(report.absent_user_id)}</dd>
        <dt>Reported</dt>
        <dd>{formatDateTime(report.reported_at)}</dd>
        {report.reviewed_at && (
          <>
            <dt>Decided</dt>
            <dd>{formatDateTime(report.reviewed_at)}</dd>
          </>
        )}
      </dl>

      <h2>What the reporter says</h2>
      <p className="preserve-lines">{report.description}</p>
      <p className="muted">
        This is an allegation, not a fact. If the other participant also reported this session, that report is a separate entry in the
        list and is decided on its own.
      </p>

      {report.resolution_note && (
        <>
          <h2>Decision note</h2>
          <p className="preserve-lines">{report.resolution_note}</p>
        </>
      )}

      {report.status === 'PENDING' && (
        <DecisionForm
          id={report.id}
          onDecided={(updated) => {
            setDone(
              updated.status === 'CONFIRMED'
                ? 'No-show confirmed. A violation was recorded for the absent participant.'
                : 'Report rejected. The session’s outcome stays as it was.',
            );
            setLoad({ version, status: 'ready', report: updated });
          }}
          onConflict={(message) => {
            setConflict(message);
            setVersion((n) => n + 1);
          }}
        />
      )}
    </>
  );
}

interface DecisionFormProps {
  id: string;
  onDecided: (report: NoShowReport) => void;
  onConflict: (message: string) => void;
}

type Decision = 'confirm' | 'reject';

/** One note for either decision. Confirming asks once more, because it records a violation. */
function DecisionForm({ id, onDecided, onConflict }: DecisionFormProps) {
  const [note, setNote] = useState('');
  const [noteError, setNoteError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState<Decision | null>(null);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function checkNote(): boolean {
    const problem = noteProblem(note);
    setNoteError(problem);
    setFormError(null);
    if (problem) {
      setAsking(false);
      signalFailure();
      return false;
    }
    return true;
  }

  async function decide(decision: Decision) {
    if (!checkNote()) return;
    setBusy(decision);
    try {
      const text = note.trim();
      onDecided(decision === 'confirm' ? await noShowApi.confirm(id, text) : await noShowApi.reject(id, text));
    } catch (error) {
      setBusy(null);
      setAsking(false);
      if (isConflict(error)) {
        onConflict(decisionError(error));
        return;
      }
      setFormError(decisionError(error));
      signalFailure();
    }
  }

  return (
    <section aria-labelledby="decision-heading">
      <h2 id="decision-heading">Decision</h2>
      <ul className="muted">
        <li>
          <strong>Confirm</strong> records a violation for the absent participant. If the session was confirmed or completed, it is
          corrected to a no-show and no longer counts in the tutor’s completed sessions, hours or ratings. Two violations in the
          same semester (confirmed no-shows or late cancellations) flag the person for their home-faculty coordinator; nobody is
          suspended automatically.
        </li>
        <li>
          <strong>Reject</strong> records your decision and leaves the session as it is.
        </li>
      </ul>
      <form
        ref={formRef}
        noValidate
        className="form"
        aria-label="Decide no-show report"
        onSubmit={(event: FormEvent<HTMLFormElement>) => {
          event.preventDefault();
          if (checkNote()) setAsking(true);
        }}
      >
        {formError && (
          <div ref={alertRef} tabIndex={-1}>
            <Alert>{formError}</Alert>
          </div>
        )}
        <TextArea
          label="Decision note"
          name="resolution-note"
          rows={4}
          value={note}
          onChange={(event) => {
            setNote(event.target.value);
            setNoteError(undefined);
          }}
          error={noteError}
          hint="What you checked and why you decided this way. It is stored with the report for coordinators."
        />
        {asking ? (
          <div className="card" role="group" aria-label="Confirm the no-show?">
            <p>
              <strong>Confirm the no-show?</strong> A violation is recorded for the absent participant. This can’t be undone here.
            </p>
            <div className="button-row">
              <Button autoFocus onClick={() => void decide('confirm')} loading={busy === 'confirm'}>
                Yes, confirm no-show
              </Button>
              <Button variant="secondary" disabled={busy !== null} onClick={() => setAsking(false)}>
                Go back
              </Button>
            </div>
          </div>
        ) : (
          <div className="button-row">
            <Button type="submit" disabled={busy !== null}>
              Confirm no-show
            </Button>
            <Button variant="secondary" loading={busy === 'reject'} onClick={() => void decide('reject')}>
              Reject report
            </Button>
          </div>
        )}
      </form>
    </section>
  );
}
