import { STATUS_LABEL } from './rules';
import type { NoShowStatus } from './types';

/** A confirmed no-show (a violation was recorded) uses the warning colour; the others the default badge. */
export function NoShowStatusBadge({ status }: { status: NoShowStatus }) {
  return <span className={status === 'CONFIRMED' ? 'badge badge--rejected' : 'badge'}>{STATUS_LABEL[status]}</span>;
}
