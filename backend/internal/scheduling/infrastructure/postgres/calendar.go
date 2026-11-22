package postgres

import (
	"context"
	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	"github.com/jackc/pgx/v5"
	"time"
)

type calendarQueries struct{ tx pgx.Tx }

func Calendar(tx pgx.Tx) catalog.CalendarScheduling { return &calendarQueries{tx: tx} }

func (q *calendarQueries) Inspect(ctx context.Context, current, proposed domain.Semester, at time.Time) (catalog.CalendarActivity, error) {
	r := catalog.CalendarActivity{Counts: map[string]int64{}, Sessions: map[string]int64{}, SessionsOutside: map[string]int64{}}
	// One-off and detached slots have no semester FK. Show geographical-in-time
	// candidates separately from linked recurring slots, and compare the exact
	// existing booking selector (whole-slot containment, latest start then ID).
	rows, err := q.tx.Query(ctx, `
WITH rules AS (SELECT * FROM availability_rules WHERE semester_id=$1),
slots AS (SELECT s.* FROM availability_slots s JOIN rules r ON r.id=s.rule_id),
sessions AS (SELECT * FROM tutoring_sessions WHERE semester_id=$1),
proposed_calendar AS (
 SELECT id,starts_at,ends_at FROM semesters WHERE id<>$1
 UNION ALL SELECT $1::uuid,$4::timestamptz,$5::timestamptz
),
oneoffs AS (
 SELECT s.*, before.id AS before_id, after.id AS after_id
 FROM availability_slots s
 LEFT JOIN LATERAL (SELECT c.id FROM semesters c WHERE c.starts_at<=s.starts_at AND c.ends_at>=s.ends_at ORDER BY c.starts_at DESC,c.id LIMIT 1) before ON true
 LEFT JOIN LATERAL (SELECT c.id FROM proposed_calendar c WHERE c.starts_at<=s.starts_at AND c.ends_at>=s.ends_at ORDER BY c.starts_at DESC,c.id LIMIT 1) after ON true
 WHERE s.rule_id IS NULL AND ((s.starts_at<$3 AND s.ends_at>$2) OR (s.starts_at<$5 AND s.ends_at>$4))
)
SELECT 'linked_rules',count(*) FROM rules
UNION ALL SELECT 'rules_affected_by_bound_change',count(*) FROM rules WHERE $2::timestamptz<>$4 OR $3::timestamptz<>$5
UNION ALL SELECT 'rule_tutors_requiring_review',count(DISTINCT tutor_id) FROM rules
UNION ALL SELECT 'exceptions_retained',count(*) FROM availability_exceptions e JOIN rules r ON r.id=e.rule_id
UNION ALL SELECT 'linked_materialized_slots',count(*) FROM slots
UNION ALL SELECT 'linked_slots_outside_proposed_bounds',count(*) FROM slots WHERE starts_at<$4 OR ends_at>$5
UNION ALL SELECT 'linked_future_slots',count(*) FROM slots WHERE ends_at>$6
UNION ALL SELECT 'oneoff_or_detached_candidates',count(*) FROM oneoffs
UNION ALL SELECT 'oneoff_candidates_contained_by_current_bounds',count(*) FROM oneoffs WHERE starts_at>=$2 AND ends_at<=$3
UNION ALL SELECT 'oneoff_candidates_outside_proposed_bounds',count(*) FROM oneoffs WHERE starts_at<$4 OR ends_at>$5
UNION ALL SELECT 'oneoff_candidates_contained_by_proposed_bounds',count(*) FROM oneoffs WHERE starts_at>=$4 AND ends_at<=$5
UNION ALL SELECT 'oneoff_candidates_future',count(*) FROM oneoffs WHERE ends_at>$6
UNION ALL SELECT 'oneoff_future_booking_selector_changes',count(*) FROM oneoffs WHERE ends_at>$6 AND before_id IS DISTINCT FROM after_id
UNION ALL SELECT 'oneoff_tutors_requiring_review',count(DISTINCT tutor_id) FROM oneoffs
UNION ALL SELECT 'active_reservation_blocks',count(*) FROM session_participant_blocks b JOIN sessions s ON s.id=b.session_id
UNION ALL SELECT 'reservation_blocks_outside_proposed_bounds',count(*) FROM session_participant_blocks b JOIN sessions s ON s.id=b.session_id WHERE s.starts_at<$4 OR s.ends_at>$5
UNION ALL SELECT 'future_active_sessions',count(*) FROM sessions WHERE status IN ('REQUESTED','ACCEPTED') AND ends_at>$6
UNION ALL SELECT 'session_history_rows_retained',count(*) FROM session_status_history h JOIN sessions s ON s.id=h.session_id
`, current.ID, current.StartsAt, current.EndsAt, proposed.StartsAt, proposed.EndsAt, at)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var name string
		var n int64
		if err := rows.Scan(&name, &n); err != nil {
			rows.Close()
			return r, err
		}
		r.Counts[name] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	rows, err = q.tx.Query(ctx, `SELECT status::text,count(*),count(*) FILTER (WHERE starts_at<$2 OR ends_at>$3)
 FROM tutoring_sessions WHERE semester_id=$1 GROUP BY status`, current.ID, proposed.StartsAt, proposed.EndsAt)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var total, outside int64
		if err := rows.Scan(&status, &total, &outside); err != nil {
			return r, err
		}
		r.Sessions[status] = total
		r.SessionsOutside[status] = outside
	}
	return r, rows.Err()
}

func (q *calendarQueries) RolloverTutors(ctx context.Context, from, to domain.Semester) (map[string]int64, error) {
	// A rule is explicit semester ownership. One-off publication evidence uses
	// the same deterministic selector as booking; it is not retroactive ownership.
	var prior, next, both int64
	err := q.tx.QueryRow(ctx, `
WITH publication AS (
 SELECT tutor_id,semester_id FROM availability_rules WHERE semester_id IN ($1,$2)
 UNION
 SELECT s.tutor_id,c.id FROM availability_slots s
 JOIN LATERAL (SELECT id FROM semesters WHERE starts_at<=s.starts_at AND ends_at>=s.ends_at ORDER BY starts_at DESC,id LIMIT 1) c ON true
 WHERE s.rule_id IS NULL AND c.id IN ($1,$2)
), previous AS (SELECT DISTINCT tutor_id FROM publication WHERE semester_id=$1),
following AS (SELECT DISTINCT tutor_id FROM publication WHERE semester_id=$2)
SELECT (SELECT count(*) FROM previous),(SELECT count(*) FROM following),
(SELECT count(*) FROM previous JOIN following USING(tutor_id))`, from.ID, to.ID).Scan(&prior, &next, &both)
	return map[string]int64{"previous_publishers": prior, "next_publishers": next, "previous_with_next_publication": both, "previous_without_next_publication": prior - both, "previous_requiring_manual_reconfirmation": prior}, err
}
