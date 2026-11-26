import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router';
import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { Alert, Button, Spinner, TextArea } from '../../shared/ui';
import { browserTimeZone } from '../../shared/utils/datetime';
import { issueReportsApi } from './api';
import { descriptionProblem, reportError, reportReference } from './rules';
import type { IssueReport, ReportedSession } from './types';

type SessionLoad = { status: 'ready'; session: ReportedSession } | { status: 'error'; error: unknown };

interface ReportFormProps {
  /** From ?session= when the student came from a session card; null for a general report. Render with key={sessionId}. */
  sessionId: string | null;
  timeZone?: string;
}

/** A general problem report to the faculty coordinators, optionally about one of the student's sessions (FR10, UC06). */
export function ReportForm({ sessionId, timeZone = browserTimeZone() }: ReportFormProps) {
  const [sessionLoad, setSessionLoad] = useState<SessionLoad | null>(null);
  const [description, setDescription] = useState('');
  const [descriptionError, setDescriptionError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [sent, setSent] = useState<IssueReport | null>(null);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  useEffect(() => {
    if (!sessionId) return;
    const controller = new AbortController();
    issueReportsApi.session(sessionId, controller.signal).then(
      (session) => setSessionLoad({ status: 'ready', session }),
      (error: unknown) => {
        if (!controller.signal.aborted) setSessionLoad({ status: 'error', error });
      },
    );
    return () => controller.abort();
  }, [sessionId]);

  const recipients = sessionId ? 'the coordinators of the tutor’s faculty' : 'the coordinators of your faculty';

  if (sessionId && !sessionLoad) return <Spinner label="Loading the session…" />;
  if (sessionId && sessionLoad?.status === 'error') {
    const missing = sessionLoad.error instanceof ApiError && (sessionLoad.error.status === 404 || sessionLoad.error.status === 422);
    return (
      <Alert
        title="This session can’t be linked to a report"
        action={
          <Link to="/report">Write a general report instead</Link>
        }
      >
        {missing ? 'It isn’t one of your sessions, or it doesn’t exist.' : errorMessage(sessionLoad.error)}
      </Alert>
    );
  }

  if (sent) {
    return (
      <>
        <Alert kind="success" title="Report sent">
          It is now in the inbox of {sent.session_id ? 'the coordinators of the tutor’s faculty' : 'the coordinators of your faculty'}.
          Sending it doesn’t resolve anything yet: a coordinator still has to review it. Your reference is{' '}
          <strong>{reportReference(sent.id)}</strong>; mention it if you contact them about this report.
        </Alert>
        <ul className="link-list">
          <li>
            <Link to="/sessions">My sessions</Link>
          </li>
          <li>
            <Link to="/">Home</Link>
          </li>
        </ul>
      </>
    );
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const problem = descriptionProblem(description);
    setDescriptionError(problem);
    setFormError(null);
    if (problem) {
      signalFailure();
      return;
    }
    setSubmitting(true);
    try {
      const report = await issueReportsApi.create({ description: description.trim(), ...(sessionId ? { session_id: sessionId } : {}) });
      setSent(report);
    } catch (error) {
      setFormError(reportError(error));
      signalFailure();
    } finally {
      setSubmitting(false);
    }
  }

  const session = sessionLoad?.status === 'ready' ? sessionLoad.session : null;
  const when = new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });

  return (
    <form ref={formRef} noValidate onSubmit={(event) => void submit(event)} className="form" aria-label="Report a problem">
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      {session ? (
        <div className="card">
          <p>
            About your <strong>{session.subject_name}</strong> session on {when.format(new Date(session.starts_at))}.
          </p>
          <Link to="/report">Report something else instead</Link>
        </div>
      ) : (
        <p className="muted">
          About a specific session? Open it in <Link to="/sessions">My sessions</Link> and choose “Report a problem” there.
        </p>
      )}
      <TextArea
        label="What happened?"
        name="description"
        rows={6}
        value={description}
        onChange={(event) => {
          setDescription(event.target.value);
          setDescriptionError(undefined);
        }}
        error={descriptionError}
        hint={`Describe the problem in your own words. Only ${recipients} can read it.`}
      />
      <div className="button-row">
        <Button type="submit" loading={submitting}>
          Send report
        </Button>
      </div>
    </form>
  );
}
