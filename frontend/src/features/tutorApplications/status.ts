import type { ApplicationStatus } from './types';

export const APPLICATION_STATUSES: ApplicationStatus[] = ['PENDING', 'APPROVED', 'REJECTED'];

/** Badge text. Students and coordinators see the same words. */
export const STATUS_LABELS: Record<ApplicationStatus, string> = {
  PENDING: 'Waiting for review',
  APPROVED: 'Approved',
  REJECTED: 'Not approved',
};

export function isApplicationStatus(value: string): value is ApplicationStatus {
  return (APPLICATION_STATUSES as string[]).includes(value);
}
