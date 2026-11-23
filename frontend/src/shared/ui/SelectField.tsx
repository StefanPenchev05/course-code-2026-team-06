import { useId, type ReactNode, type SelectHTMLAttributes } from 'react';

interface SelectFieldProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, 'id'> {
  label: string;
  error?: string;
  hint?: string;
  children: ReactNode;
}

/** Labelled native <select> that links its hint and error for screen readers, like TextField. */
export function SelectField({ label, error, hint, className, children, ...rest }: SelectFieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined;

  return (
    <div className={['field', error ? 'field--invalid' : '', className].filter(Boolean).join(' ')}>
      <label className="field__label" htmlFor={id}>
        {label}
      </label>
      {hint && (
        <p className="field__hint" id={hintId}>
          {hint}
        </p>
      )}
      <select {...rest} id={id} className="field__input" aria-invalid={error ? true : undefined} aria-describedby={describedBy}>
        {children}
      </select>
      {error && (
        <p className="field__error" id={errorId}>
          {error}
        </p>
      )}
    </div>
  );
}
