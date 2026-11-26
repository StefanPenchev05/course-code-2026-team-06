import { STATUS_LABEL } from './rules';
import type { VerificationStatus } from './types';

/** Pending uses the default badge colour; approved and rejected reuse the #30 styles. */
export function VerificationStatusBadge({ status }: { status: VerificationStatus }) {
  return <span className={`badge badge--${status.toLowerCase()}`}>{STATUS_LABEL[status]}</span>;
}
