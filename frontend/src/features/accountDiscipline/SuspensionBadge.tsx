/** Current account state: suspended accounts use the warning colour. */
export function SuspensionBadge({ suspended }: { suspended: boolean }) {
  return <span className={suspended ? 'badge badge--rejected' : 'badge badge--active'}>{suspended ? 'Suspended' : 'Not suspended'}</span>;
}
