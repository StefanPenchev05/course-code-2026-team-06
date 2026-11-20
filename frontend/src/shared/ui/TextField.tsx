import { useId, type InputHTMLAttributes } from 'react';

interface TextFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'id'> {
  label: string;
  error?: string;
  hint?: string;
}

/** Labelled input that links its hint and error to the field for screen readers. */
export function TextField({ label, error, hint, className, ...rest }: TextFieldProps) {
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
      <input {...rest} id={id} className="field__input" aria-invalid={error ? true : undefined} aria-describedby={describedBy} />
      {error && (
        <p className="field__error" id={errorId}>
          {error}
        </p>
      )}
    </div>
  );
}
