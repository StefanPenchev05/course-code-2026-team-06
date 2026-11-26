import { STATUS_LABEL } from './rules';
import type { IssueStatus } from './types';

/** Open uses the default badge colour; resolved reuses the "approved" style. */
export function IssueStatusBadge({ status }: { status: IssueStatus }) {
  return <span className={`badge${status === 'RESOLVED' ? ' badge--approved' : ''}`}>{STATUS_LABEL[status]}</span>;
}
