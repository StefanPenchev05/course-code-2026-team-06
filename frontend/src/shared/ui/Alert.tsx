import type { ReactNode } from 'react';

interface AlertProps {
  kind?: 'error' | 'info' | 'success' | 'warning';
  title?: string;
  children?: ReactNode;
  action?: ReactNode;
}

/** Errors use role="alert" so screen readers announce them immediately. */
export function Alert({ kind = 'error', title, children, action }: AlertProps) {
  return (
    <div className={`alert alert--${kind}`} role={kind === 'error' ? 'alert' : 'status'}>
      {title && <p className="alert__title">{title}</p>}
      {children && <div className="alert__body">{children}</div>}
      {action && <div className="alert__action">{action}</div>}
    </div>
  );
}
