import { useEffect, useRef, useState } from 'react';
import { Button } from './Button';

interface ConfirmActionProps {
  /** Text of the first button, e.g. "Remove". */
  label: string;
  /** The question shown before acting, e.g. "Remove this time?". */
  question: string;
  /** Text of the confirming button, e.g. "Yes, remove". */
  confirmLabel: string;
  /** Runs the action. Throw to keep the question open (the caller shows the error). */
  onConfirm: () => Promise<void>;
  /** Accessible name for the first button when the label alone is ambiguous in a list. */
  ariaLabel?: string;
}

/** A destructive action that asks once, inline, before it runs. Keyboard focus follows the question. */
export function ConfirmAction({ label, question, confirmLabel, onConfirm, ariaLabel }: ConfirmActionProps) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const confirmRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (asking) confirmRef.current?.focus();
  }, [asking]);

  if (!asking) {
    return (
      <Button variant="link" onClick={() => setAsking(true)} aria-label={ariaLabel}>
        {label}
      </Button>
    );
  }

  async function confirm() {
    setBusy(true);
    try {
      await onConfirm();
      setAsking(false);
    } catch {
      // The caller already shows what went wrong; keep the choice open.
    } finally {
      setBusy(false);
    }
  }

  return (
    <span className="confirm" role="group" aria-label={question}>
      <span>{question}</span>{' '}
      <Button ref={confirmRef} variant="secondary" onClick={() => void confirm()} loading={busy}>
        {confirmLabel}
      </Button>{' '}
      <Button variant="link" onClick={() => setAsking(false)} disabled={busy}>
        Cancel
      </Button>
    </span>
  );
}
