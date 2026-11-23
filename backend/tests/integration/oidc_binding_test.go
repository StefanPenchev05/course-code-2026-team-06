package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestOIDCIdentityBindings(t *testing.T) {
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("use test_identity.sh for migrated PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClient(opts)
	defer cache.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	query := func(sql string, args ...any) string {
		t.Helper()
		var result string
		if err := db.QueryRow(ctx, sql, args...).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	count := func(want int, sql string, args ...any) {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, sql, args...).Scan(&n); err != nil || n != want {
			t.Fatalf("count %d want %d: %v", n, want, err)
		}
	}
	const issuer = "http://localhost:8180/realms/tutti-demo"
	repo := identitypg.New(db)
	resolver, err := application.NewOIDCIdentityResolver(repo, issuer)
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := application.NewTrustedOIDCProvisioner(repo, issuer)
	if err != nil {
		t.Fatal(err)
	}
	id := func(subject string) domain.VerifiedIdentity {
		return domain.VerifiedIdentity{Issuer: issuer, Subject: subject}
	}
	exec("INSERT INTO faculties(id,name) VALUES($1,'OIDC binding faculty')", fixture(19001))
	exec("INSERT INTO programs(id,faculty_id,name,level) VALUES($1,$2,'OIDC program','BACHELOR')", fixture(19002), fixture(19001))
	for n := 19101; n <= 19116; n++ {
		exec("INSERT INTO users(id,name,email) VALUES($1,'Synthetic identical display name',$2)", fixture(n), "oidc-binding-"+fixture(n)+"@tutti.example")
	}
	exec("INSERT INTO coordinators(user_id,employee_number,faculty_id) VALUES($1,'OIDC-COORD',$2)", fixture(19101), fixture(19001))
	for n := 19102; n <= 19116; n++ {
		if n == 19111 {
			continue
		} // account without any platform role
		exec("INSERT INTO students(user_id,student_number,program_id) VALUES($1,$2,$3)", fixture(n), "OIDC-STUDENT-"+fixture(n), fixture(19002))
		status := "APPROVED"
		if n == 19103 {
			status = "PENDING"
		}
		if n == 19104 {
			status = "REJECTED"
		}
		exec(`INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id,rejection_reason)
   VALUES($1,$2,$3::text::verification_status,CASE WHEN $3::text='PENDING' THEN NULL ELSE now() END,CASE WHEN $3::text='PENDING' THEN NULL ELSE $4::uuid END,CASE WHEN $3::text='REJECTED' THEN 'Synthetic rejection' ELSE NULL END)`, fixture(n), fixture(19001), status, fixture(19101))
	}
	for _, n := range []int{19105, 19106} {
		exec("INSERT INTO tutor_profiles(student_id,description,approved_at,is_active) VALUES($1,'Synthetic tutor',now(),$2)", fixture(n), n == 19105)
	}
	exec("INSERT INTO subjects(id,code,name) VALUES($1,'OIDC-SUBJECT','Synthetic subject')", fixture(19003))
	exec("INSERT INTO program_subjects VALUES($1,$2)", fixture(19002), fixture(19003))
	exec(`INSERT INTO tutor_applications(id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id)
 VALUES($1,$2,$3,'Synthetic approval','APPROVED',now(),$4)`, fixture(19004), fixture(19105), fixture(19001), fixture(19101))
	exec("INSERT INTO tutor_qualifications(id,application_id,student_id,subject_id,grade) VALUES($1,$2,$3,$4,18.50)", fixture(19005), fixture(19004), fixture(19105), fixture(19003))
	exec("INSERT INTO tutor_offerings(id,tutor_id,subject_id,qualification_id,description) VALUES($1,$2,$3,$4,'Synthetic offering')", fixture(19006), fixture(19105), fixture(19003), fixture(19005))
	// Exact snapshots of existing account, academic, tutor and historical records.
	snapshot := func() string {
		return query(`SELECT md5(jsonb_build_array(
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM users t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id) FROM students t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id) FROM coordinators t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM student_verifications t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY student_id) FROM tutor_profiles t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM tutor_applications t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM tutor_qualifications t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM tutor_offerings t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM tutoring_sessions t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM session_status_history t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM application_status_history t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM user_suspension_history t),
  (SELECT jsonb_agg(to_jsonb(t) ORDER BY session_id) FROM feedback t)
 )::text)`)
	}
	mappings := func() string {
		return query("SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]')::text) FROM oidc_identities t")
	}
	hexUser := "00000000-0000-0000-0000-00000000ab12"
	exec("INSERT INTO users(id,name,email) VALUES($1,'Synthetic UUID owner','uuid-owner@tutti.example')", hexUser)
	exec("INSERT INTO students(user_id,student_number,program_id) VALUES($1,'OIDC-HEX-UUID',$2)", hexUser, fixture(19002))
	before := snapshot()
	sessionsBefore, err := cache.Keys(ctx, "tutti:identity:session:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{19101, 19102, 19103, 19104, 19105, 19106, 19107, 19108} {
		external := id("synthetic-sub-" + fixture(n))
		if err := provisioner.LinkOIDCIdentity(ctx, fixture(n), external); err != nil {
			t.Fatal(err)
		}
		p, err := resolver.ResolveOIDCIdentity(ctx, external)
		if err != nil || p.ID != fixture(n) {
			t.Fatal("identity resolution", err)
		}
		switch n {
		case 19101:
			if !p.Coordinator() || p.CoordinatorFaculty != fixture(19001) || p.Student {
				t.Fatal("coordinator authority changed")
			}
		case 19103, 19104:
			if p.VerifiedStudent() || p.CanTutor() {
				t.Fatal("unapproved student elevated")
			}
		case 19105:
			if !p.CanTutor() || p.ProgramID != fixture(19002) {
				t.Fatal("approved tutor lost authority")
			}
		case 19106:
			if p.CanTutor() || !p.VerifiedStudent() {
				t.Fatal("inactive tutor changed authority")
			}
		default:
			if !p.VerifiedStudent() || p.CanTutor() {
				t.Fatal("student privileges changed")
			}
		}
	}
	if snapshot() != before {
		t.Fatal("binding/resolution changed existing accounts/evidence")
	}
	if err := provisioner.LinkOIDCIdentity(ctx, "00000000-0000-0000-0000-00000000AB12", id("hex-uuid-subject")); err != nil {
		t.Fatal(err)
	}
	hexMapping := mappings()
	if err := provisioner.LinkOIDCIdentity(ctx, hexUser, id("hex-uuid-subject")); err != nil || mappings() != hexMapping {
		t.Fatal("equivalent UUID changed binding", err)
	}
	stable := mappings()
	if err := provisioner.LinkOIDCIdentity(ctx, fixture(19102), id("synthetic-sub-"+fixture(19102))); err != nil {
		t.Fatal("duplicate identical UUID mapping", err)
	}
	if mappings() != stable {
		t.Fatal("idempotent link changed ID or created_at")
	}
	expectLink := func(user string, identity domain.VerifiedIdentity, want error) {
		t.Helper()
		beforeAccount, beforeMap := snapshot(), mappings()
		err := provisioner.LinkOIDCIdentity(ctx, user, identity)
		if !errors.Is(err, want) {
			t.Fatalf("expected %v, got %v", want, err)
		}
		if snapshot() != beforeAccount || mappings() != beforeMap {
			t.Fatal("failed link wrote data")
		}
	}
	expectLink(fixture(19109), id("synthetic-sub-"+fixture(19102)), domain.ErrConflict)
	expectLink(fixture(19102), id("new-subject"), domain.ErrConflict)
	expectLink(fixture(19999), id("unknown-user"), domain.ErrNotFound)
	expectLink(fixture(19111), id("roleless"), domain.ErrForbidden)
	expectLink(fixture(19109), domain.VerifiedIdentity{Issuer: issuer, Subject: " "}, domain.ErrInvalid)
	expectLink(fixture(19109), domain.VerifiedIdentity{Issuer: issuer + "/wrong", Subject: "same"}, domain.ErrInvalid)
	if p, err := resolver.ResolveOIDCIdentity(ctx, id("unknown-subject")); !errors.Is(err, domain.ErrOIDCIdentityUnlinked) || p.ID != "" {
		t.Fatal("unknown subject resolved", err)
	}
	if _, err := resolver.ResolveOIDCIdentity(ctx, id("SYNTHETIC-SUB-"+fixture(19102))); !errors.Is(err, domain.ErrOIDCIdentityUnlinked) {
		t.Fatal("subject comparison folded case", err)
	}
	other, _ := application.NewOIDCIdentityResolver(repo, issuer+"/other")
	if _, err := other.ResolveOIDCIdentity(ctx, domain.VerifiedIdentity{Issuer: issuer + "/other", Subject: "synthetic-sub-" + fixture(19102)}); !errors.Is(err, domain.ErrOIDCIdentityUnlinked) {
		t.Fatal("wrong issuer resolved", err)
	}
	// Mutable names/emails never change the binding, including misleading aliases.
	exec("UPDATE users SET email='renamed@tutti.example',name='Renamed synthetic user' WHERE id=$1", fixture(19102))
	p, err := resolver.ResolveOIDCIdentity(ctx, id("synthetic-sub-"+fixture(19102)))
	if err != nil || p.ID != fixture(19102) || p.Email != "renamed@tutti.example" {
		t.Fatal("mutable claims broke stable binding", err)
	}
	if _, err := resolver.ResolveOIDCIdentity(ctx, id("renamed@tutti.example")); !errors.Is(err, domain.ErrOIDCIdentityUnlinked) {
		t.Fatal("email-based account linking occurred")
	}
	// Account status is always current. Even identical links cannot bypass disablement.
	exec("UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Synthetic restriction' WHERE id=$1", fixture(19107), fixture(19101))
	exec("UPDATE users SET closed_at=now() WHERE id=$1", fixture(19108))
	for _, n := range []int{19107, 19108} {
		external := id("synthetic-sub-" + fixture(n))
		if p, err := resolver.ResolveOIDCIdentity(ctx, external); !errors.Is(err, domain.ErrForbidden) || p.ID != "" {
			t.Fatal("disabled identity resolved", err)
		}
		expectLink(fixture(n), external, domain.ErrForbidden)
	}
	// First links to disabled accounts also fail with no persistent mapping.
	exec("UPDATE users SET closed_at=now() WHERE id=$1", fixture(19109))
	expectLink(fixture(19109), id("closed-unlinked"), domain.ErrForbidden)
	exec("UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Synthetic restriction' WHERE id=$1", fixture(19110), fixture(19101))
	expectLink(fixture(19110), id("suspended-unlinked"), domain.ErrForbidden)
	race := func(users []string, subjects []string, successes int) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, len(users))
		for i, user := range users {
			go func(user, subject string) { <-start; results <- provisioner.LinkOIDCIdentity(ctx, user, id(subject)) }(user, subjects[i])
		}
		close(start)
		passed := 0
		for range users {
			err := <-results
			if err == nil {
				passed++
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Fatal(err)
			}
		}
		if passed != successes {
			t.Fatal("unexpected race result", passed, successes)
		}
	}
	before = snapshot()
	race([]string{fixture(19112), fixture(19113)}, []string{"contested-subject", "contested-subject"}, 1)
	count(1, "SELECT count(*) FROM oidc_identities WHERE issuer=$1 AND subject='contested-subject'", issuer)
	race([]string{fixture(19114), fixture(19114)}, []string{"contested-a", "contested-b"}, 1)
	count(1, "SELECT count(*) FROM oidc_identities WHERE user_id=$1", fixture(19114))
	sameUsers, sameSubjects := []string{}, []string{}
	for range 8 {
		sameUsers = append(sameUsers, fixture(19115))
		sameSubjects = append(sameSubjects, "idempotent-concurrent")
	}
	race(sameUsers, sameSubjects, 8)
	count(1, "SELECT count(*) FROM oidc_identities WHERE user_id=$1", fixture(19115))
	if snapshot() != before {
		t.Fatal("concurrent linking changed account/history records")
	}
	// A real insert-time database failure must roll back its mapping.
	func() {
		exec(`CREATE FUNCTION oidc_binding_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic binding failure'; END $$`)
		defer exec("DROP FUNCTION oidc_binding_test_fail()")
		exec("CREATE TRIGGER oidc_binding_test_fail AFTER INSERT ON oidc_identities FOR EACH ROW EXECUTE FUNCTION oidc_binding_test_fail()")
		defer exec("DROP TRIGGER oidc_binding_test_fail ON oidc_identities")
		old := mappings()
		if err := provisioner.LinkOIDCIdentity(ctx, fixture(19116), id("injected-failure")); err == nil {
			t.Fatal("injected failure succeeded")
		}
		if mappings() != old {
			t.Fatal("failed insert left a mapping")
		}
	}()
	// A status writer that takes the established account lock wins before linking.
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE", fixture(19116)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE users SET closed_at=now() WHERE id=$1", fixture(19116)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- provisioner.LinkOIDCIdentity(ctx, fixture(19116), id("racing-close")) }()
	waitEligibilityBlock(t, ctx, db, tx.Conn().PgConn().PID())
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("link bypassed concurrent closure", err)
	}
	count(0, "SELECT count(*) FROM oidc_identities WHERE user_id=$1", fixture(19116))
	sessionsAfter, err := cache.Keys(ctx, "tutti:identity:session:*").Result()
	if err != nil || len(sessionsAfter) != len(sessionsBefore) {
		t.Fatal("binding created application sessions", err)
	}
	// Resolution itself is read-only, including status failures and repeated lookups.
	before, stable = snapshot(), mappings()
	resolver.ResolveOIDCIdentity(ctx, id("unknown-subject"))
	resolver.ResolveOIDCIdentity(ctx, id("synthetic-sub-"+fixture(19105)))
	if before != snapshot() || stable != mappings() {
		t.Fatal("lookup wrote persistence")
	}
}
