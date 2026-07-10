// Package store persists webhook tokens, receivers, notifications and audit
// rows in a single SQLite database file using the pure-Go modernc.org/sqlite
// driver (no CGO). The connection pool is capped at one open connection so
// writes are serialized and "database is locked" cannot occur on this
// single-instance MVP.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/makia9879/makia-notice/internal/auth"
	_ "modernc.org/sqlite"
)

// Receiver is the persisted view of a delivery target.
type Receiver struct {
	ReceiverID        string
	IdentityTokenHash string
	Allowlisted       bool
	Enabled           bool
	Revoked           bool
	ProviderType      string
}

// Notification is the persisted view of an accepted notification.
type Notification struct {
	NotificationID string
	TokenID        string
	ReceiverID     string
	IdempotencyKey string
	ContentHash    string
	Title          string
	Body           string
	Priority       string
	GroupKey       string
	DataJSON       string
	TTLSeconds     int
	ExpiresAt      time.Time
	Status         string
	CreatedAt      time.Time
}

// Audit row recorded for every webhook/test request.
type Audit struct {
	RequestID  string
	TokenID    string
	ReceiverID string
	Provider   string
	StatusCode int
	ErrorClass string
	DurationMS int64
	CreatedAt  time.Time
}

// CreateOutcome classifies the result of CreateNotification.
type CreateOutcome int

const (
	OutcomeCreated CreateOutcome = iota
	OutcomeDuplicate
	OutcomeConflict
)

// ErrNotFound is returned by getters when no row matches.
var ErrNotFound = errors.New("not found")

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open connects to the database at path, applies migrations and configures the
// connection pool. It creates parent directories as needed.
func Open(path string) (*Store, error) {
	if err := ensureParentDir(path); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection => serialized writes, no lock contention for MVP.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS webhook_tokens (
			token_id    TEXT PRIMARY KEY,
			token_hash  TEXT NOT NULL,
			enabled     INTEGER NOT NULL DEFAULT 1,
			revoked     INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS receivers (
			receiver_id           TEXT PRIMARY KEY,
			identity_token_hash   TEXT NOT NULL,
			allowlisted           INTEGER NOT NULL DEFAULT 1,
			enabled               INTEGER NOT NULL DEFAULT 1,
			revoked               INTEGER NOT NULL DEFAULT 0,
			provider_type         TEXT NOT NULL DEFAULT 'self_hosted_sse',
			created_at            TEXT NOT NULL,
			updated_at            TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			notification_id  TEXT PRIMARY KEY,
			token_id         TEXT NOT NULL,
			receiver_id      TEXT NOT NULL,
			idempotency_key  TEXT NOT NULL DEFAULT '',
			content_hash     TEXT NOT NULL,
			title            TEXT NOT NULL,
			body             TEXT NOT NULL,
			priority         TEXT NOT NULL,
			group_key        TEXT NOT NULL DEFAULT '',
			data_json        TEXT NOT NULL DEFAULT '{}',
			ttl_seconds      INTEGER NOT NULL,
			expires_at       TEXT NOT NULL,
			status           TEXT NOT NULL DEFAULT 'accepted',
			created_at       TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notif_idem ON notifications(token_id, receiver_id, idempotency_key)`,
		`CREATE TABLE IF NOT EXISTS audit (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id   TEXT NOT NULL,
			token_id     TEXT,
			receiver_id  TEXT,
			provider     TEXT,
			status_code  INTEGER NOT NULL,
			error_class  TEXT,
			duration_ms  INTEGER NOT NULL,
			created_at   TEXT NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}
	return nil
}

// UpsertWebhookToken inserts or updates the bootstrap webhook token row.
func (s *Store) UpsertWebhookToken(ctx context.Context, tokenID, hash string, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO webhook_tokens(token_id, token_hash, enabled, revoked, created_at, updated_at)
		VALUES(?, ?, ?, 0, ?, ?)
		ON CONFLICT(token_id) DO UPDATE SET token_hash=excluded.token_hash, enabled=excluded.enabled, updated_at=excluded.updated_at`,
		tokenID, hash, boolToInt(enabled), now, now)
	return err
}

// UpsertReceiver inserts or updates a receiver (bootstrap) row.
func (s *Store) UpsertReceiver(ctx context.Context, r Receiver) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO receivers(receiver_id, identity_token_hash, allowlisted, enabled, revoked, provider_type, created_at, updated_at)
		VALUES(?, ?, ?, ?, 0, ?, ?, ?)
		ON CONFLICT(receiver_id) DO UPDATE SET identity_token_hash=excluded.identity_token_hash, allowlisted=excluded.allowlisted, enabled=excluded.enabled, provider_type=excluded.provider_type, updated_at=excluded.updated_at`,
		r.ReceiverID, r.IdentityTokenHash, boolToInt(r.Allowlisted), boolToInt(r.Enabled), r.ProviderType, now, now)
	return err
}

// EnabledWebhookTokens loads all enabled, non-revoked webhook tokens for the
// in-memory auth checker.
func (s *Store) EnabledWebhookTokens(ctx context.Context) ([]auth.WebhookToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token_id, token_hash, enabled, revoked FROM webhook_tokens WHERE enabled=1 AND revoked=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.WebhookToken
	for rows.Next() {
		var t auth.WebhookToken
		var en, rv int
		if err := rows.Scan(&t.TokenID, &t.Hash, &en, &rv); err != nil {
			return nil, err
		}
		t.Enabled = en == 1
		t.Revoked = rv == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetReceiver loads a receiver by id.
func (s *Store) GetReceiver(ctx context.Context, receiverID string) (*Receiver, error) {
	row := s.db.QueryRowContext(ctx, `SELECT receiver_id, identity_token_hash, allowlisted, enabled, revoked, provider_type FROM receivers WHERE receiver_id=?`, receiverID)
	var r Receiver
	var al, en, rv int
	if err := row.Scan(&r.ReceiverID, &r.IdentityTokenHash, &al, &en, &rv, &r.ProviderType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.Allowlisted = al == 1
	r.Enabled = en == 1
	r.Revoked = rv == 1
	return &r, nil
}

// CheckIdempotency performs a read-only lookup of an existing notification for
// the given (tokenID, receiverID, key) within the retention window. It returns
// Duplicate when an entry with matching content hash exists, Conflict when an
// entry with a different content hash exists, and Created when none exists.
func (s *Store) CheckIdempotency(ctx context.Context, tokenID, receiverID, key, contentHash string, window time.Duration) (CreateOutcome, string, error) {
	if key == "" {
		return OutcomeCreated, "", nil
	}
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)
	var existingID, existingHash string
	err := s.db.QueryRowContext(ctx,
		`SELECT notification_id, content_hash FROM notifications
		  WHERE token_id=? AND receiver_id=? AND idempotency_key=? AND created_at >= ?
		  ORDER BY created_at DESC LIMIT 1`,
		tokenID, receiverID, key, cutoff).Scan(&existingID, &existingHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OutcomeCreated, "", nil
		}
		return 0, "", err
	}
	if existingHash == contentHash {
		return OutcomeDuplicate, existingID, nil
	}
	return OutcomeConflict, existingID, nil
}

// CreateNotification persists a new notification under the idempotency rules.
// The lookup-and-insert is performed inside a single transaction so concurrent
// duplicate requests cannot both create rows.
func (s *Store) CreateNotification(ctx context.Context, n Notification, idempotencyWindow time.Duration) (CreateOutcome, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }()

	if n.IdempotencyKey != "" {
		cutoff := time.Now().UTC().Add(-idempotencyWindow).Format(time.RFC3339)
		var existingID, existingHash string
		err := tx.QueryRowContext(ctx,
			`SELECT notification_id, content_hash FROM notifications
			  WHERE token_id=? AND receiver_id=? AND idempotency_key=? AND created_at >= ?
			  ORDER BY created_at DESC LIMIT 1`,
			n.TokenID, n.ReceiverID, n.IdempotencyKey, cutoff).Scan(&existingID, &existingHash)
		if err == nil {
			if existingHash == n.ContentHash {
				return OutcomeDuplicate, existingID, tx.Commit()
			}
			return OutcomeConflict, existingID, tx.Commit()
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", err
		}
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO notifications(notification_id, token_id, receiver_id, idempotency_key, content_hash, title, body, priority, group_key, data_json, ttl_seconds, expires_at, status, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.NotificationID, n.TokenID, n.ReceiverID, n.IdempotencyKey, n.ContentHash,
		n.Title, n.Body, n.Priority, n.GroupKey, n.DataJSON, n.TTLSeconds,
		n.ExpiresAt.UTC().Format(time.RFC3339), "accepted", now.Format(time.RFC3339))
	if err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return OutcomeCreated, n.NotificationID, nil
}

// InsertAudit writes a single audit row. Errors are returned but must not
// affect the response path; callers log them.
func (s *Store) InsertAudit(ctx context.Context, a Audit) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit(request_id, token_id, receiver_id, provider, status_code, error_class, duration_ms, created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		a.RequestID, nullString(a.TokenID), nullString(a.ReceiverID), nullString(a.Provider),
		a.StatusCode, nullString(a.ErrorClass), a.DurationMS, a.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

// SweepExpired marks undelivered-but-expired notifications as dropped. It is a
// best-effort housekeeping routine and never affects already-returned 201s.
func (s *Store) SweepExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET status='dropped' WHERE status='accepted' AND expires_at < ?`,
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ensureParentDir(path string) error {
	dir := parentDir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return mkdirAll(dir)
}
