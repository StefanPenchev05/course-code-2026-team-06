package postgres

import (
	"context"
	scheduleview "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application/monitoring"
	"github.com/jackc/pgx/v5"
)

type monitoring struct{ tx pgx.Tx }

func Monitoring(tx pgx.Tx) scheduleview.Queries { return &monitoring{tx} }

func (m *monitoring) Schedule(ctx context.Context, faculty, tutor string, f scheduleview.Filter) ([]scheduleview.ScheduleEntry, error) {
	// Slots already embody Scheduling's recurrence/exception/DST decisions. They
	// are published windows, not free intervals: booked sessions appear separately.
	rows, err := m.tx.Query(ctx, `
		SELECT id,kind,starts_at,ends_at,mode,status,subject_id,subject_name,rule_id,time_zone,semester_id FROM (
			SELECT sl.id::text,'AVAILABILITY' AS kind,sl.starts_at,sl.ends_at,sl.allowed_mode::text AS mode,
				'' AS status,'' AS subject_id,'' AS subject_name,coalesce(sl.rule_id::text,'') AS rule_id,
				coalesce(r.time_zone,'') AS time_zone,coalesce(r.semester_id::text,'') AS semester_id
			FROM availability_slots sl LEFT JOIN availability_rules r ON r.id=sl.rule_id
			JOIN students st ON st.user_id=sl.tutor_id JOIN programs p ON p.id=st.program_id
			WHERE sl.tutor_id=$1 AND p.faculty_id=$2 AND sl.ends_at>$3 AND sl.starts_at<$4
			UNION ALL
			SELECT s.id::text,'SESSION',s.starts_at,s.ends_at,s.mode::text,s.status::text,
				sub.id::text,sub.name,'','',s.semester_id::text
			FROM tutoring_sessions s JOIN tutor_offerings o ON o.id=s.offering_id JOIN subjects sub ON sub.id=o.subject_id
			WHERE s.tutor_id=$1 AND s.faculty_id=$2 AND s.ends_at>$3 AND s.starts_at<$4
		) entries ORDER BY starts_at,kind,id LIMIT $5 OFFSET $6`, tutor, faculty, f.From, f.To, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (scheduleview.ScheduleEntry, error) {
		var v scheduleview.ScheduleEntry
		err := row.Scan(&v.ID, &v.Kind, &v.StartsAt, &v.EndsAt, &v.Mode, &v.Status, &v.SubjectID, &v.SubjectName, &v.RuleID, &v.TimeZone, &v.SemesterID)
		v.StartsAt, v.EndsAt = v.StartsAt.UTC(), v.EndsAt.UTC()
		return v, err
	})
}
