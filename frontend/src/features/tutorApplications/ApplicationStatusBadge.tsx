import { STATUS_LABELS } from './status';
import type { ApplicationStatus } from './types';

export function ApplicationStatusBadge({ status }: { status: ApplicationStatus }) {
  return <span className={`badge badge--${status.toLowerCase()}`}>{STATUS_LABELS[status]}</span>;
}
