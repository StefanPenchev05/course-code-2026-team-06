import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * After a failed form submit, moves keyboard focus (and the scroll position)
 * to the form-level message, or else to the first field marked
 * aria-invalid. The problem may be above the submit button, out of view.
 *
 * Put `formRef` on the <form>, `alertRef` on a focusable wrapper
 * (tabIndex={-1}) around the form-level message, and call `signalFailure()`
 * whenever a submit fails.
 */
export function useFocusOnFailure() {
  const [failures, setFailures] = useState(0);
  const formRef = useRef<HTMLFormElement>(null);
  const alertRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (failures === 0) return;
    const target = alertRef.current ?? formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]');
    target?.focus();
  }, [failures]);

  const signalFailure = useCallback(() => setFailures((n) => n + 1), []);
  return { formRef, alertRef, signalFailure };
}
