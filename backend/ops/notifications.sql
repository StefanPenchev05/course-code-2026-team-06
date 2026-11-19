-- Operator-only, read-only diagnostics. No recipients, content, or claim tokens.
BEGIN READ ONLY;
SET LOCAL statement_timeout = '5s';
SELECT
    count(*) FILTER (WHERE email_status = 'PENDING') AS pending,
    count(*) FILTER (WHERE email_status = 'PENDING' AND next_attempt_at <= statement_timestamp()
        AND (lease_until IS NULL OR lease_until <= statement_timestamp())) AS due_or_exhausted,
    count(*) FILTER (WHERE email_status = 'PENDING' AND lease_until > statement_timestamp()) AS leased,
    count(*) FILTER (WHERE email_status = 'PENDING' AND next_attempt_at > statement_timestamp()
        AND lease_until IS NULL) AS awaiting_retry,
    count(*) FILTER (WHERE email_status = 'PENDING' AND lease_until <= statement_timestamp()) AS expired_leases,
    count(*) FILTER (WHERE email_status = 'FAILED') AS failed,
    count(*) FILTER (WHERE email_status = 'SENT') AS sent,
    extract(epoch FROM statement_timestamp() - min(created_at) FILTER (WHERE email_status = 'PENDING'))::bigint AS oldest_pending_seconds,
    max(email_sent_at) AS last_sent_at
FROM notifications;

SELECT email_status, last_error, count(*) AS notifications, max(attempt_count) AS most_attempts,
       min(next_attempt_at) AS earliest_retry, max(email_failed_at) AS latest_failure
FROM notifications
WHERE email_status <> 'SENT'
GROUP BY email_status, last_error
ORDER BY email_status, last_error;
COMMIT;
