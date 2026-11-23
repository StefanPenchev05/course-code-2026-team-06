import type { ButtonHTMLAttributes, Ref } from 'react';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'link';
  loading?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

export function Button({ variant = 'primary', loading = false, disabled, children, className, type = 'button', ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      type={type}
      className={['button', `button--${variant}`, className].filter(Boolean).join(' ')}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
    >
      {loading && <span className="spinner__icon spinner__icon--small" aria-hidden="true" />}
      {children}
    </button>
  );
}
