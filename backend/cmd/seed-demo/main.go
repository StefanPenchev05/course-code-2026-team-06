// seed-demo provisions fictional accounts in an explicitly selected local database.
// It is never invoked by the API, migrations, or container startup.
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	"github.com/jackc/pgx/v5"
)

//go:embed seed.sql
var seedSQL string

func main() {
	local := flag.Bool("local-demo", false, "confirm the target is a disposable local demo database")
	flag.Parse()
	if err := run(*local); err != nil {
		// Connection errors can include credentials; do not print them.
		fmt.Fprintln(os.Stderr, "Demo seed failed:", err)
		os.Exit(1)
	}
}

func run(local bool) error {
	if !local {
		return errors.New("requires --local-demo; never use demo accounts in production")
	}
	config, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil || os.Getenv("DATABASE_URL") == "" {
		return errors.New("set DATABASE_URL to a migrated local demo database")
	}
	if config.Host != "localhost" && config.Host != "127.0.0.1" && config.Host != "::1" {
		return errors.New("only loopback database hosts are allowed")
	}
	password := os.Getenv("DEMO_PASSWORD")
	if len(password) < 12 || len(password) > 128 {
		return errors.New("set DEMO_PASSWORD to 12–128 bytes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return errors.New("could not connect to local database")
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		return errors.New("could not start seed transaction")
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(6002026)"); err != nil {
		return errors.New("could not lock demo seed")
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE
		(id='f6000000-0000-4000-8000-000000000010' AND email='coordinator@example.test') OR
		(id='f6000000-0000-4000-8000-000000000011' AND email='student@example.test') OR
		(id='f6000000-0000-4000-8000-000000000012' AND email='tutor@example.test') OR
		(id='f6000000-0000-4000-8000-000000000013' AND email='pending@example.test')`).Scan(&count); err != nil {
		return errors.New("could not inspect demo accounts; apply migrations first")
	}
	if count == 4 {
		fmt.Println("Demo accounts already exist; passwords, approvals and availability left unchanged.")
		return nil
	}
	if count != 0 {
		return errors.New("partial demo accounts exist; refusing to overwrite them")
	}
	hasher, err := security.NewPasswords(security.Cost{Memory: 65536, Time: 3, Threads: 2}, 1)
	if err != nil {
		return err
	}
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		return errors.New("could not hash demo password")
	}
	// Set the password as a bound value; SQL never interpolates the plaintext.
	if _, err := tx.Exec(ctx, "SELECT set_config('tutti.demo_password_hash', $1, true)", hash); err != nil {
		return errors.New("could not prepare demo password hash")
	}
	if _, err := tx.Exec(ctx, seedSQL); err != nil {
		return errors.New("could not insert demo data; transaction rolled back (check migrations or conflicting demo records)")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("could not commit demo data")
	}
	fmt.Println("Created coordinator@example.test, student@example.test, tutor@example.test, pending@example.test.")
	return nil
}
