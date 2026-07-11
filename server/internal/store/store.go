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

	"github.com/makia9879/electricwave/internal/auth"
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

	// Reliable delivery fields (0007-integration-contract §1,§7,§8).
	EventID      int64
	AcceptedAt   time.Time
	FirstSentAt  time.Time
	LastSentAt   time.Time
	AckedAt      time.Time
	AttemptCount int
	LastError    string
}

// notificationColumns is the fixed column list used for full-row scans, kept in
// the same order as the CREATE TABLE statement.
const notificationColumns = `notification_id, token_id, receiver_id, idempotency_key, content_hash,
	title, body, priority, group_key, data_json, ttl_seconds, expires_at,
	status, created_at, event_id, accepted_at, first_sent_at, last_sent_at,
	acked_at, attempt_count, last_error`

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
	OutcomeBacklogFull
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
			created_at       TEXT NOT NULL,
			event_id         INTEGER NOT NULL DEFAULT 0,
			accepted_at      TEXT NOT NULL DEFAULT '',
			first_sent_at    TEXT NOT NULL DEFAULT '',
			last_sent_at     TEXT NOT NULL DEFAULT '',
			acked_at         TEXT NOT NULL DEFAULT '',
			attempt_count    INTEGER NOT NULL DEFAULT 0,
			last_error       TEXT NOT NULL DEFAULT ''
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

	// Idempotent column additions for databases created before the reliable
	// delivery migration. ALTER TABLE ADD COLUMN fails if the column already
	// exists, so we check PRAGMA table_info first.
	existing, err := s.columnsOf("notifications")
	if err != nil {
		return fmt.Errorf("inspect notifications columns: %w", err)
	}
	for _, c := range []struct{ name, decl string }{
		{"event_id", "INTEGER NOT NULL DEFAULT 0"},
		{"accepted_at", "TEXT NOT NULL DEFAULT ''"},
		{"first_sent_at", "TEXT NOT NULL DEFAULT ''"},
		{"last_sent_at", "TEXT NOT NULL DEFAULT ''"},
		{"acked_at", "TEXT NOT NULL DEFAULT ''"},
		{"attempt_count", "INTEGER NOT NULL DEFAULT 0"},
		{"last_error", "TEXT NOT NULL DEFAULT ''"},
	} {
		if existing[c.name] {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE notifications ADD COLUMN %s %s", c.name, c.decl)
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}
	// Created AFTER the column additions: on databases created before the
	// reliable-delivery migration, event_id does not exist until the ALTER TABLE
	// loop above has run, so the index must be built last.
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notif_replay ON notifications(receiver_id, event_id)`); err != nil {
		return fmt.Errorf("exec %q: %w", "CREATE INDEX idx_notif_replay", err)
	}
	return nil
}

// columnsOf returns the set of column names that already exist on table.
func (s *Store) columnsOf(table string) (map[string]bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
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
// duplicate requests cannot both create rows. event_id is allocated inside the
// same transaction as the INSERT (§1). When backlogMax > 0, the receiver's
// active backlog (accepted+queued, not expired) is counted before the insert;
// if the limit is already reached no row is created and OutcomeBacklogFull is
// returned (§6).
func (s *Store) CreateNotification(ctx context.Context, n Notification, idempotencyWindow time.Duration, backlogMax int) (CreateOutcome, string, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", 0, err
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
				return OutcomeDuplicate, existingID, 0, tx.Commit()
			}
			return OutcomeConflict, existingID, 0, tx.Commit()
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", 0, err
		}
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Capacity guard (§6): count not-yet-delivered backlog for this receiver.
	if backlogMax > 0 {
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM notifications
			  WHERE receiver_id=? AND status IN ('accepted','queued') AND expires_at > ?`,
			n.ReceiverID, nowStr).Scan(&count); err != nil {
			return 0, "", 0, err
		}
		if count >= backlogMax {
			return OutcomeBacklogFull, "", 0, nil
		}
	}

	// Allocate the next per-receiver event_id inside the same transaction (§1).
	var eventID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(event_id),0)+1 FROM notifications WHERE receiver_id=?`,
		n.ReceiverID).Scan(&eventID); err != nil {
		return 0, "", 0, err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO notifications(
		notification_id, token_id, receiver_id, idempotency_key, content_hash,
		title, body, priority, group_key, data_json, ttl_seconds, expires_at,
		status, created_at, event_id, accepted_at, first_sent_at, last_sent_at,
		acked_at, attempt_count, last_error)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.NotificationID, n.TokenID, n.ReceiverID, n.IdempotencyKey, n.ContentHash,
		n.Title, n.Body, n.Priority, n.GroupKey, n.DataJSON, n.TTLSeconds,
		n.ExpiresAt.UTC().Format(time.RFC3339), "accepted", nowStr,
		eventID, nowStr, "", "", "", 0, "")
	if err != nil {
		return 0, "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", 0, err
	}
	return OutcomeCreated, n.NotificationID, eventID, nil
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

// SweepExpired transitions notifications whose TTL has elapsed to the expired
// state. Per §6 any notification in accepted/queued/sent whose expires_at is in
// the past is marked expired. This is a best-effort housekeeping routine.
func (s *Store) SweepExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET status='expired'
		 WHERE status IN ('accepted','queued','sent') AND expires_at < ?`,
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteAckedOlder removes acked notifications older than cutoff, enforcing the
// 24-hour hard retention ceiling (§6). Safe under the single-connection model.
func (s *Store) DeleteAckedOlder(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM notifications WHERE status='acked' AND created_at < ?`,
		cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// MarkSent transitions a notification to sent, populating first_sent_at on the
// initial delivery and updating last_sent_at / attempt_count on every delivery
// (§7). Used by both the webhook (accepted→sent) and replay (queued/sent→sent).
func (s *Store) MarkSent(ctx context.Context, notificationID string) error {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET status='sent',
		 first_sent_at=CASE WHEN first_sent_at='' THEN ? ELSE first_sent_at END,
		 last_sent_at=?, attempt_count=attempt_count+1
		 WHERE notification_id=?`, nowStr, nowStr, notificationID)
	return err
}

// MarkQueued transitions a notification to queued (accepted→queued when the
// receiver is offline or delivery did not succeed).
func (s *Store) MarkQueued(ctx context.Context, notificationID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET status='queued' WHERE notification_id=?`, notificationID)
	return err
}

// ApplyAck marks every queued/sent notification with event_id <= ackEventID as
// acked (§4.3). It is idempotent: re-acking an old value is a no-op and never
// returns an error. ackEventID <= 0 is a no-op.
func (s *Store) ApplyAck(ctx context.Context, receiverID string, ackEventID int64) (int64, error) {
	if ackEventID <= 0 {
		return 0, nil
	}
	nowStr := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET status='acked', acked_at=?
		 WHERE receiver_id=? AND event_id<=? AND status IN ('queued','sent')`,
		nowStr, receiverID, ackEventID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// BacklogSummary is the data emitted in the SSE info control event (§2.2).
type BacklogSummary struct {
	AckedEventID         int64 // 0 when no acked event exists (rendered as null)
	OldestUnackedEventID int64 // 0 when none (rendered as null)
	NewestEventID        int64 // 0 when none (rendered as null)
	Count                int   // backlog_count: queued/sent, not expired
	// OldestUnackedAcceptedAt is the RFC3339 accepted_at of the oldest unacked
	// active notification (the "最老积压时间" diagnostic, §10.4). Empty when none.
	OldestUnackedAcceptedAt string
}

// BacklogSummary computes the info-event fields in a single scan (§2.2, §4.6).
func (s *Store) BacklogSummary(ctx context.Context, receiverID string, now time.Time) (BacklogSummary, error) {
	nowStr := now.UTC().Format(time.RFC3339)
	var sum BacklogSummary
	err := s.db.QueryRowContext(ctx,
		`SELECT
			COALESCE(MAX(CASE WHEN status='acked' THEN event_id END), 0),
			COALESCE(MIN(CASE WHEN status IN ('queued','sent') AND expires_at > ? THEN event_id END), 0),
			COALESCE(MAX(CASE WHEN status IN ('queued','sent') AND expires_at > ? THEN event_id END), 0),
			COALESCE(SUM(CASE WHEN status IN ('queued','sent') AND expires_at > ? THEN 1 ELSE 0 END), 0),
			COALESCE(MIN(CASE WHEN status IN ('queued','sent') AND expires_at > ? THEN accepted_at END), '')
		 FROM notifications WHERE receiver_id=?`,
		nowStr, nowStr, nowStr, nowStr, receiverID).Scan(
		&sum.AckedEventID, &sum.OldestUnackedEventID, &sum.NewestEventID, &sum.Count, &sum.OldestUnackedAcceptedAt)
	if err != nil {
		return BacklogSummary{}, err
	}
	return sum, nil
}

// BacklogCount returns the count of unacked active notifications (queued/sent,
// not expired) for the receiver. Used for the 202 queued response body.
func (s *Store) BacklogCount(ctx context.Context, receiverID string, now time.Time) (int, error) {
	nowStr := now.UTC().Format(time.RFC3339)
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications
		 WHERE receiver_id=? AND status IN ('queued','sent') AND expires_at > ?`,
		receiverID, nowStr).Scan(&count)
	return count, err
}

// ReplaySet returns the notifications to replay after cursor, in ascending
// event_id order (§4.5). Only queued/sent rows that have not expired are
// included.
func (s *Store) ReplaySet(ctx context.Context, receiverID string, cursor int64, now time.Time) ([]Notification, error) {
	nowStr := now.UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+notificationColumns+`
		 FROM notifications
		 WHERE receiver_id=? AND event_id > ? AND status IN ('queued','sent') AND expires_at > ?
		 ORDER BY event_id ASC`,
		receiverID, cursor, nowStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parseTime parses an RFC3339 timestamp stored as TEXT; an empty string yields
// the zero time (meaning "not set" for the nullable sent/acked timestamps).
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// scanner is satisfied by *sql.Rows and *sql.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanNotification reads a full notification row in notificationColumns order.
func scanNotification(sc scanner) (Notification, error) {
	var n Notification
	var expiresAt, createdAt, acceptedAt, firstSentAt, lastSentAt, ackedAt string
	err := sc.Scan(
		&n.NotificationID, &n.TokenID, &n.ReceiverID, &n.IdempotencyKey, &n.ContentHash,
		&n.Title, &n.Body, &n.Priority, &n.GroupKey, &n.DataJSON, &n.TTLSeconds,
		&expiresAt, &n.Status, &createdAt,
		&n.EventID, &acceptedAt, &firstSentAt, &lastSentAt, &ackedAt,
		&n.AttemptCount, &n.LastError,
	)
	if err != nil {
		return Notification{}, err
	}
	n.ExpiresAt = parseTime(expiresAt)
	n.CreatedAt = parseTime(createdAt)
	n.AcceptedAt = parseTime(acceptedAt)
	n.FirstSentAt = parseTime(firstSentAt)
	n.LastSentAt = parseTime(lastSentAt)
	n.AckedAt = parseTime(ackedAt)
	return n, nil
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
