export function Spinner({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="spinner" role="status" aria-live="polite">
      <span className="spinner__icon" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

export function PageLoader({ label }: { label?: string }) {
  return (
    <div className="page-loader">
      <Spinner label={label} />
    </div>
  );
}
