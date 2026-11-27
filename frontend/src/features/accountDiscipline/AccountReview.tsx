import { useEffect, useState, type FormEvent } from 'react';
import { errorMessage, noLongerVisible } from '../../shared/api/errors';
import { useFocusOnFailure } from '../../shared/hooks/useFocusOnFailure';
import { useLiveRefresh } from '../../shared/realtime';
import { Alert, Button, Spinner, TextArea } from '../../shared/ui';
import { AccountHistory } from './AccountHistory';
import { disciplineApi } from './api';
import { decisionError, formatDateTime, isConflict, reasonProblem, reference } from './rules';
import { SuspensionBadge } from './SuspensionBadge';
import type { AuditEvent, SuspensionDecision } from './types';

type Latest = { version: number; status: 'ready'; event: AuditEvent | null } | { version: number; status: 'error'; message: string };

/**
 * One account seen by its faculty's coordinator: the latest suspension decision, suspend or
 * reinstate (FR19, UC14), and the account's history (NFR12). Render it with key={userId}.
 */
export function AccountReview({ userId }: { userId: string }) {
  const [version, setVersion] = useState(0);
  const [latest, setLatest] = useState<Latest | null>(null);
  const [notice, setNotice] = useState<{ kind: 'success' | 'warning' | 'info'; text: string } | null>(null);
  // Counts this coordinator's own decisions; a new one resets the form and the history.
  const [decisions, setDecisions] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    disciplineApi.history(userId, 'SUSPENSION', 0, controller.signal).then(
      (page) => setLatest({ version, status: 'ready', event: page.data[0] ?? null }),
      (error: unknown) => {
        if (!controller.signal.aborted) setLatest({ version, status: 'error', message: errorMessage(error) });
      },
    );
    return () => controller.abort();
  }, [userId, version]);

  // Live refresh (#93): a suspension or reinstatement decided elsewhere shows up in place.
  // coordination.changed carries a decision or flag ID, not the account, so any of them
  // re-checks. The reason being typed stays unless the account's state flipped.
  useLiveRefresh(['coordination.changed'], () => {
    const before = latest?.status === 'ready' ? latest.event?.is_suspended === true : null;
    disciplineApi.history(userId, 'SUSPENSION', 0).then(
      (page) => {
        const event = page.data[0] ?? null;
        setLatest((previous) => (previous?.status === 'ready' ? { ...previous, event } : previous));
        const now = event?.is_suspended === true;
        if (before !== null && before !== now) {
          setNotice({
            kind: 'info',
            text: now
              ? 'This account was suspended elsewhere while you had it open, for example in another tab.'
              : 'This account was reinstated elsewhere while you had it open, for example in another tab.',
          });
        }
      },
      (error: unknown) => {
        if (noLongerVisible(error)) setLatest({ version, status: 'error', message: errorMessage(error) });
      },
    );
  });

  function decided(decision: SuspensionDecision) {
    setNotice({
      kind: 'success',
      text: decision.is_suspended
        ? `Account suspended. ${decision.cancelled_sessions} future ${decision.cancelled_sessions === 1 ? 'session was' : 'sessions were'} cancelled; the person and the other participants are notified.`
        : 'Account reinstated. The person can use Tutti again; cancelled sessions and flags stay as they are.',
    });
    setDecisions((n) => n + 1);
    setVersion((n) => n + 1);
  }

  const current = latest?.version === version || latest?.status === 'ready' ? latest : null;
  const suspended = current?.status === 'ready' && current.event?.is_suspended === true;

  return (
    <>
      <h1>Account {reference(userId)}</h1>
      {notice && <Alert kind={notice.kind}>{notice.text}</Alert>}

      <section aria-labelledby="state-heading">
        <h2 id="state-heading">Suspension</h2>
        {!current && <Spinner label="Loading the account’s state…" />}
        {current?.status === 'error' && (
          <Alert
            action={
              <Button variant="secondary" onClick={() => setVersion((n) => n + 1)}>
                Try again
              </Button>
            }
          >
            {current.message}
          </Alert>
        )}
        {current?.status === 'ready' && (
          <>
            <p>
              {current.event && (
                <>
                  <SuspensionBadge suspended={suspended} />{' '}
                </>
              )}
              {current.event
                ? `Last decision by your faculty: ${current.event.is_suspended ? 'suspended' : 'reinstated'} on ${formatDateTime(current.event.recorded_at)}.`
                : 'Your faculty has not suspended this account.'}
            </p>
            <p className="muted">
              Flags never suspend anyone automatically. Only the coordinator of the person’s current home faculty can suspend or
              reinstate them.
            </p>
            <DecisionForm
              key={`${suspended}-${decisions}`}
              userId={userId}
              suspend={!suspended}
              onDecided={decided}
              onConflict={(message) => {
                setNotice({ kind: 'warning', text: message });
                setDecisions((n) => n + 1);
                setVersion((n) => n + 1);
              }}
            />
          </>
        )}
      </section>

      <AccountHistory key={decisions} userId={userId} />
    </>
  );
}

interface DecisionFormProps {
  userId: string;
  /** true: offer suspension; false: offer reinstatement. */
  suspend: boolean;
  onDecided: (decision: SuspensionDecision) => void;
  onConflict: (message: string) => void;
}

function DecisionForm({ userId, suspend, onDecided, onConflict }: DecisionFormProps) {
  const [reason, setReason] = useState('');
  const [reasonError, setReasonError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const { formRef, alertRef, signalFailure } = useFocusOnFailure();

  function checkReason(): boolean {
    const problem = reasonProblem(reason);
    setReasonError(problem);
    setFormError(null);
    if (problem) {
      setAsking(false);
      signalFailure();
    }
    return !problem;
  }

  async function send() {
    if (!checkReason()) return;
    setBusy(true);
    try {
      const text = reason.trim();
      onDecided(suspend ? await disciplineApi.suspend(userId, text) : await disciplineApi.reinstate(userId, text));
    } catch (error) {
      setBusy(false);
      setAsking(false);
      if (isConflict(error)) {
        onConflict(decisionError(error));
        return;
      }
      setFormError(decisionError(error));
      signalFailure();
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!checkReason()) return;
    if (suspend) setAsking(true);
    else void send();
  }

  return (
    <form ref={formRef} noValidate onSubmit={submit} className="form" aria-label={suspend ? 'Suspend account' : 'Reinstate account'}>
      {formError && (
        <div ref={alertRef} tabIndex={-1}>
          <Alert>{formError}</Alert>
        </div>
      )}
      {suspend ? (
        <ul className="muted">
          <li>The person loses access to Tutti at once, in every role (student, tutor).</li>
          <li>
            All their future requested and confirmed sessions are cancelled, as student and as tutor. Both participants are notified
            with a general message; your reason stays private.
          </li>
          <li>It stays in place until a coordinator reinstates the account. It doesn’t count as a violation for anyone.</li>
        </ul>
      ) : (
        <ul className="muted">
          <li>The person can use Tutti again, as far as their registration and tutor approval still allow.</li>
          <li>Cancelled sessions are not reopened, and flags and violations stay in the history.</li>
        </ul>
      )}
      <TextArea
        label={suspend ? 'Reason for suspending' : 'Reason for reinstating'}
        name="reason"
        rows={4}
        value={reason}
        onChange={(event) => {
          setReason(event.target.value);
          setReasonError(undefined);
        }}
        error={reasonError}
        hint="Private: kept in the account’s history for coordinators of your faculty."
      />
      {asking ? (
        <div className="card" role="group" aria-label="Suspend this account?">
          <p>
            <strong>Suspend this account?</strong> Access is blocked and future sessions are cancelled now.
          </p>
          <div className="button-row">
            <Button autoFocus onClick={() => void send()} loading={busy}>
              Yes, suspend
            </Button>
            <Button variant="secondary" disabled={busy} onClick={() => setAsking(false)}>
              Go back
            </Button>
          </div>
        </div>
      ) : (
        <div className="button-row">
          <Button type="submit" loading={busy}>
            {suspend ? 'Suspend account' : 'Reinstate account'}
          </Button>
        </div>
      )}
    </form>
  );
}
