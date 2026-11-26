import { useEffect, useState, type FormEvent } from 'react';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, PageLoader, TextArea } from '../../shared/ui';
import { issuesApi } from './api';
import { IssueStatusBadge } from './IssueStatusBadge';
import { formatDateTime, isConflict, loadError, noteProblem, reference, resolveError } from './rules';
import type { Issue } from './types';

type Load = { version: number; status: 'ready'; issue: Issue } | { version: number; status: 'error'; error: unknown };

/** One problem report of the coordinator's faculty, with the one-time resolution (FR17, UC13). Render it with key={id}. */
export function IssueReview({ id }: { id: string }) {
  const [version, setVersion] = useState(0);
  const [load, setLoad] = useState<Load | null>(null);
  const [resolved, setResolved] = useState(false);
  const [conflict, setConflict] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    issuesApi.detail(id, controller.signal).then(
      (issue) => setLoad({ version, status: 'ready', issue }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLoad({ version, status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [id, version]);

  // After an error, "Try again" shows the loader; after a conflict the last state stays until the new one arrives.
  if (!load || (load.status === 'error' && load.version !== version)) {
    return (
      <>
        <h1>Problem report</h1>
        <PageLoader label="Loading report…" />
      </>
    );
  }
  if (load.status === 'error') {
    return (
      <>
        <h1>Problem report</h1>
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

  const { issue } = load;

  return (
    <>
      <h1>Report {reference(issue.id)}</h1>
      {resolved && <Alert kind="success">Report resolved. Your note is stored with it.</Alert>}
      {conflict && <Alert kind="warning">{conflict}</Alert>}

      <dl className="details">
        <dt>Status</dt>
        <dd>
          <IssueStatusBadge status={issue.status} />
        </dd>
        <dt>Sent</dt>
        <dd>{formatDateTime(issue.created_at)}</dd>
        <dt>About</dt>
        <dd>{issue.session_id ? `A session (ref. ${reference(issue.session_id)})` : 'No specific session'}</dd>
        <dt>Reporter ref.</dt>
        <dd>{reference(issue.reporter_user_id)}</dd>
        {issue.resolved_at && (
          <>
            <dt>Resolved</dt>
            <dd>{formatDateTime(issue.resolved_at)}</dd>
          </>
        )}
      </dl>

      <h2>What happened</h2>
      <p className="preserve-lines">{issue.description}</p>

      {issue.resolution_note && (
        <>
          <h2>Resolution note</h2>
          <p className="preserve-lines">{issue.resolution_note}</p>
        </>
      )}

      {issue.status === 'OPEN' && (
        <ResolveForm
          id={issue.id}
          onResolved={(updated) => {
            setResolved(true);
            setLoad({ version, status: 'ready', issue: updated });
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

interface ResolveFormProps {
  id: string;
  onResolved: (issue: Issue) => void;
  onConflict: (message: string) => void;
}

function ResolveForm({ id, onResolved, onConflict }: ResolveFormProps) {
  const [note, setNote] = useState('');
  const [noteError, setNoteError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const problem = noteProblem(note);
    setNoteError(problem);
    setFormError(null);
    if (problem) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      onResolved(await issuesApi.resolve(id, note.trim()));
    } catch (error) {
      setSubmitting(false);
      // Someone else resolved it: the page reloads, shows the stored note and explains why at the top.
      if (isConflict(error)) {
        onConflict(resolveError(error));
        return;
      }
      setFormError(resolveError(error));
      signalFailure();
    }
  }

  return (
    <section aria-labelledby="resolve-heading">
      <h2 id="resolve-heading">Resolve</h2>
      <p className="muted">
        Resolving closes the report for good; it can’t be reopened or edited. It doesn’t change any session, attendance record or
        account, and Tutti doesn’t notify the reporter.
      </p>
      <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form" aria-label="Resolve report">
        {formError && (
          <div ref={alertRef} tabIndex={-1}>
            <Alert>{formError}</Alert>
          </div>
        )}
        <TextArea
          label="Resolution note"
          name="resolution-note"
          rows={4}
          value={note}
          onChange={(event) => {
            setNote(event.target.value);
            setNoteError(undefined);
          }}
          error={noteError}
          hint="What you checked, did or decided. It is stored with the report for other coordinators."
        />
        <div className="button-row">
          <Button type="submit" loading={submitting}>
            Resolve report
          </Button>
        </div>
      </form>
    </section>
  );
}
