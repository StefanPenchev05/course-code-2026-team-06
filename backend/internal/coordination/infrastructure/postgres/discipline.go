package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
)

type discipline struct{ tx pgx.Tx }

func Discipline(tx pgx.Tx) application.Discipline { return discipline{tx} }

func (d discipline) RecordLateCancellation(ctx context.Context, v application.LateCancellation) error {
	_, err := d.tx.Exec(ctx, `
		INSERT INTO user_violations (user_id,semester_id,session_id,kind,occurred_at,
		    recorded_at,actor_kind,recorded_by_user_id,home_faculty_id)
		VALUES ($1,$2,$3,'LATE_CANCELLATION',$4,$4,'SYSTEM',NULL,$5)
		ON CONFLICT (user_id,session_id) DO NOTHING`, v.UserID, v.SemesterID, v.SessionID, v.OccurredAt, v.HomeFacultyID)
	if err != nil {
		return err
	}
	return d.flagAtThreshold(ctx, v.UserID, v.SemesterID, v.HomeFacultyID, v.OccurredAt)
}

func (d discipline) recordNoShow(ctx context.Context, user, semester, session, home, report, reviewer string, occurred, decided time.Time) error {
	// Let a conflicting incident abort the decision instead of confirming a
	// report with no matching disciplinary record.
	_, err := d.tx.Exec(ctx, `
		INSERT INTO user_violations (user_id,semester_id,session_id,kind,occurred_at,
		                            attendance_report_id,confirmed_by_coordinator_id,
		                            recorded_at,actor_kind,recorded_by_user_id,home_faculty_id)
		VALUES ($1,$2,$3,'NO_SHOW',$4,$5,$6,$7,'USER',$6,$8)`, user, semester, session, occurred, report, reviewer, decided, home)
	if err != nil {
		return err
	}
	return d.flagAtThreshold(ctx, user, semester, home, decided)
}

func (d discipline) flagAtThreshold(ctx context.Context, user, semester, home string, at time.Time) error {
	_, err := d.tx.Exec(ctx, `
		WITH basis AS (
			SELECT array_agg(id ORDER BY recorded_at,id) AS ids FROM (
				SELECT id,recorded_at FROM user_violations WHERE user_id=$1 AND semester_id=$2
				ORDER BY recorded_at,id LIMIT 2
			) v
		), flagged AS (
			INSERT INTO user_semester_flags (user_id,semester_id,faculty_id,flagged_at,actor_kind,first_violation_id,second_violation_id)
			SELECT $1,$2,$3,$4,'SYSTEM',ids[1],ids[2] FROM basis WHERE cardinality(ids)=2
			ON CONFLICT (user_id,semester_id) DO NOTHING
			RETURNING id,faculty_id
		)
		INSERT INTO notifications (recipient_user_id,type,subject,body,flag_id,deduplication_key)
		SELECT c.user_id,'USER_FLAGGED','Semester violation threshold reached',
		       'Sign in to Tutti to review the semester flag.',f.id,'semester-flag:'||f.id::text
		FROM flagged f JOIN coordinators c ON c.faculty_id=f.faculty_id`,
		user, semester, home, at)
	return err
}
