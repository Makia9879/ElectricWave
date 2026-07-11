package store

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "notice.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

var idCounter int64

// mustCreate creates a notification for receiverID with a 1-hour TTL and returns
// its notification_id and event_id.
func mustCreate(t *testing.T, st *Store, ctx context.Context, receiverID string, backlogMax int) (string, int64) {
	t.Helper()
	seq := atomic.AddInt64(&idCounter, 1)
	n := Notification{
		NotificationID: "ntf_test_" + receiverID + "_" + itoa64(seq),
		TokenID:        "bootstrap",
		ReceiverID:     receiverID,
		ContentHash:    "h",
		Title:          "t", Body: "b",
		Priority: "normal", DataJSON: "{}",
		TTLSeconds: 3600,
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	out, id, eventID, err := st.CreateNotification(ctx, n, time.Hour, backlogMax)
	if err != nil || out != OutcomeCreated {
		t.Fatalf("create: out=%v err=%v", out, err)
	}
	return id, eventID
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestCreateNotificationIdempotency(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	n := Notification{
		NotificationID: "ntf_1", TokenID: "bootstrap", ReceiverID: "r",
		IdempotencyKey: "k", ContentHash: "h1", Title: "t", Body: "b",
		Priority: "normal", DataJSON: "{}", TTLSeconds: 3600,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	out, id, _, err := st.CreateNotification(ctx, n, time.Hour, 0)
	if err != nil || out != OutcomeCreated || id != "ntf_1" {
		t.Fatalf("first create: out=%v id=%s err=%v", out, id, err)
	}

	// Same key, same content -> duplicate, returns original id.
	n.NotificationID = "ntf_2"
	out, id, _, err = st.CreateNotification(ctx, n, time.Hour, 0)
	if err != nil || out != OutcomeDuplicate || id != "ntf_1" {
		t.Fatalf("duplicate: out=%v id=%s err=%v", out, id, err)
	}

	// Same key, different content -> conflict.
	n.ContentHash = "h2"
	n.NotificationID = "ntf_3"
	out, id, _, err = st.CreateNotification(ctx, n, time.Hour, 0)
	if err != nil || out != OutcomeConflict {
		t.Fatalf("conflict: out=%v id=%s err=%v", out, id, err)
	}

	// Empty idempotency key always creates.
	n.IdempotencyKey = ""
	n.NotificationID = "ntf_4"
	out, id, _, err = st.CreateNotification(ctx, n, time.Hour, 0)
	if err != nil || out != OutcomeCreated || id != "ntf_4" {
		t.Fatalf("empty-key create: out=%v id=%s err=%v", out, id, err)
	}
}

func TestCheckIdempotencyWindow(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	n := Notification{
		NotificationID: "ntf_x", TokenID: "bootstrap", ReceiverID: "r",
		IdempotencyKey: "kw", ContentHash: "hx", Title: "t", Body: "b",
		Priority: "normal", DataJSON: "{}", TTLSeconds: 3600,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if _, _, _, err := st.CreateNotification(ctx, n, time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	out, _, err := st.CheckIdempotency(ctx, "bootstrap", "r", "kw", "hx", time.Hour)
	if err != nil || out != OutcomeDuplicate {
		t.Fatalf("expected duplicate, got %v %v", out, err)
	}
	out, _, err = st.CheckIdempotency(ctx, "bootstrap", "r", "kw", "other", time.Hour)
	if err != nil || out != OutcomeConflict {
		t.Fatalf("expected conflict, got %v %v", out, err)
	}
	// No matching key.
	out, _, err = st.CheckIdempotency(ctx, "bootstrap", "r", "none", "hx", time.Hour)
	if err != nil || out != OutcomeCreated {
		t.Fatalf("expected created, got %v %v", out, err)
	}
}

func TestSweepExpired(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	n := Notification{
		NotificationID: "ntf_old", TokenID: "bootstrap", ReceiverID: "r",
		IdempotencyKey: "", ContentHash: "h", Title: "t", Body: "b",
		Priority: "normal", DataJSON: "{}", TTLSeconds: 60,
		ExpiresAt: time.Now().Add(-time.Minute), // already expired
	}
	if _, _, _, err := st.CreateNotification(ctx, n, time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	affected, err := st.SweepExpired(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if affected < 1 {
		t.Fatalf("expected at least 1 row swept, got %d", affected)
	}
}

func TestGetReceiverNotFound(t *testing.T) {
	st := openTestStore(t)
	_, err := st.GetReceiver(context.Background(), "nope")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestMigrationIdempotent verifies that re-opening a database that already has
// the reliable-delivery columns does not error (ALTER TABLE is skipped).
func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notice.db")

	st1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	ctx := context.Background()
	_, _ = mustCreate(t, st1, ctx, "r", 0)
	_ = st1.Close()

	// Re-open: migrate() runs again and must not fail on existing columns.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second open (idempotent migration): %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })

	// Verify the replay index exists.
	cols, err := st2.columnsOf("notifications")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"event_id", "accepted_at", "first_sent_at", "last_sent_at", "acked_at", "attempt_count", "last_error"} {
		if !cols[want] {
			t.Errorf("missing column %s after re-migration", want)
		}
	}
}

// TestMigrationFromOldSchema reproduces the production upgrade path: a database
// whose notifications table predates the reliable-delivery columns. migrate()
// must add the columns AND build idx_notif_replay without failing on
// "no such column: event_id". The index references event_id, so it must be
// created AFTER the ALTER TABLE additions, not before.
func TestMigrationFromOldSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notice.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Replace the table with the PRE-reliable-delivery schema and seed a row.
	if _, err := st.db.Exec(`DROP TABLE notifications; CREATE TABLE notifications (
		notification_id TEXT PRIMARY KEY, token_id TEXT NOT NULL, receiver_id TEXT NOT NULL,
		idempotency_key TEXT NOT NULL DEFAULT '', content_hash TEXT NOT NULL, title TEXT NOT NULL,
		body TEXT NOT NULL, priority TEXT NOT NULL, group_key TEXT NOT NULL DEFAULT '',
		data_json TEXT NOT NULL DEFAULT '{}', ttl_seconds INTEGER NOT NULL, expires_at TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'accepted', created_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("recreate old schema: %v", err)
	}
	if _, err := st.db.Exec(`INSERT INTO notifications(notification_id,token_id,receiver_id,content_hash,title,body,priority,ttl_seconds,expires_at,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"ntf_old", "tok", "r", "hash", "t", "b", "normal", 3600, "2030-01-01T00:00:00Z", "accepted", "2025-01-01T00:00:00Z"); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Re-open: this is the upgrade that crashed in production before the fix.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen old-schema db (migration): %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })

	cols, err := st2.columnsOf("notifications")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"event_id", "accepted_at", "acked_at", "attempt_count", "last_error"} {
		if !cols[want] {
			t.Errorf("column %s missing after migration from old schema", want)
		}
	}
	// Regression: idx_notif_replay must exist (it was built before event_id existed).
	var name string
	err = st2.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_notif_replay'`).Scan(&name)
	if err != nil || name != "idx_notif_replay" {
		t.Fatalf("idx_notif_replay missing after migration: name=%q err=%v", name, err)
	}
}

// TestEventIDMonotonic verifies per-receiver event_id is strictly increasing
// and continuous (1, 2, 3, …) and independent across receivers.
func TestEventIDMonotonic(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		_, eid := mustCreate(t, st, ctx, "r1", 0)
		if eid != int64(i) {
			t.Fatalf("r1 event_id %d: got %d, want %d", i, eid, i)
		}
	}
	// Different receiver has its own sequence starting at 1.
	for i := 1; i <= 3; i++ {
		_, eid := mustCreate(t, st, ctx, "r2", 0)
		if eid != int64(i) {
			t.Fatalf("r2 event_id %d: got %d, want %d", i, eid, i)
		}
	}
	// Back to r1: continues from where it left off.
	_, eid := mustCreate(t, st, ctx, "r1", 0)
	if eid != 6 {
		t.Fatalf("r1 event_id after interleaving: got %d, want 6", eid)
	}
}

// TestApplyAck verifies that acking transitions queued/sent rows to acked and
// is idempotent (old ack values don't error and don't re-ack).
func TestApplyAck(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	id1, e1 := mustCreate(t, st, ctx, "r", 0)
	id2, e2 := mustCreate(t, st, ctx, "r", 0)
	id3, _ := mustCreate(t, st, ctx, "r", 0)
	// e3 stays accepted (not sent/queued), so ack should NOT touch it.
	_ = id3

	// Move first two into sent/queued so ack applies.
	if err := st.MarkSent(ctx, id1); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkQueued(ctx, id2); err != nil {
		t.Fatal(err)
	}

	n, err := st.ApplyAck(ctx, "r", e2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("ack affected %d rows, want 2", n)
	}

	// Re-ack the same value: idempotent, 0 rows affected.
	n, err = st.ApplyAck(ctx, "r", e2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("re-ack affected %d rows, want 0", n)
	}

	// Ack with a smaller value: idempotent, no error.
	n, err = st.ApplyAck(ctx, "r", e1-1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("old ack affected %d rows, want 0", n)
	}

	// Zero/negative ack: no-op.
	n, err = st.ApplyAck(ctx, "r", 0)
	if err != nil || n != 0 {
		t.Fatalf("ack(0): n=%d err=%v", n, err)
	}
}

// TestReplaySet verifies the replay query returns only queued/sent rows after
// the cursor, in ascending event_id order, excluding expired rows.
func TestReplaySet(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	id1, e1 := mustCreate(t, st, ctx, "r", 0)
	id2, _ := mustCreate(t, st, ctx, "r", 0)
	id3, _ := mustCreate(t, st, ctx, "r", 0)

	// id1 → acked (excluded from replay).
	_ = st.MarkSent(ctx, id1)
	_, _ = st.ApplyAck(ctx, "r", e1)
	// id2 → sent (included).
	_ = st.MarkSent(ctx, id2)
	// id3 → stays accepted... wait, accepted is NOT in replay set.
	// Replay set only includes queued/sent. Let's mark id3 as queued.
	_ = st.MarkQueued(ctx, id3)

	replay, err := st.ReplaySet(ctx, "r", 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 2 {
		t.Fatalf("replay set size: got %d, want 2", len(replay))
	}
	// Ascending event_id order.
	if replay[0].NotificationID != id2 || replay[1].NotificationID != id3 {
		t.Fatalf("replay order wrong: %+v %+v", replay[0], replay[1])
	}

	// With cursor = event_id of id2, only id3 should remain.
	replay, err = st.ReplaySet(ctx, "r", replay[0].EventID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 1 || replay[0].NotificationID != id3 {
		t.Fatalf("cursor replay wrong: %+v", replay)
	}
}

// TestBacklogSummaryFields verifies the info-event aggregate fields.
func TestBacklogSummaryFields(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	id1, e1 := mustCreate(t, st, ctx, "r", 0)
	id2, _ := mustCreate(t, st, ctx, "r", 0)
	id3, e3 := mustCreate(t, st, ctx, "r", 0)

	_ = st.MarkSent(ctx, id1)
	_, _ = st.ApplyAck(ctx, "r", e1) // id1 acked
	_ = st.MarkSent(ctx, id2)        // id2 sent (unacked)
	_ = st.MarkQueued(ctx, id3)      // id3 queued (unacked)
	_ = e3

	sum, err := st.BacklogSummary(ctx, "r", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if sum.AckedEventID != e1 {
		t.Fatalf("acked_event_id: got %d, want %d", sum.AckedEventID, e1)
	}
	if sum.OldestUnackedEventID != e1+1 {
		t.Fatalf("oldest_unacked: got %d, want %d", sum.OldestUnackedEventID, e1+1)
	}
	if sum.NewestEventID != e3 {
		t.Fatalf("newest_event_id: got %d, want %d", sum.NewestEventID, e3)
	}
	if sum.Count != 2 {
		t.Fatalf("backlog_count: got %d, want 2", sum.Count)
	}
}

// TestBacklogCountEmpty verifies that a receiver with no active backlog
// reports zero.
func TestBacklogCountEmpty(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	count, err := st.BacklogCount(ctx, "ghost", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("empty backlog count: got %d, want 0", count)
	}
}

// TestCapacityFull verifies that CreateNotification returns OutcomeBacklogFull
// when the receiver's active backlog reaches the limit, and that sent rows do
// NOT count toward capacity.
func TestCapacityFull(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	// Create 3 accepted notifications with a cap of 3.
	mustCreate(t, st, ctx, "r", 3)
	mustCreate(t, st, ctx, "r", 3)
	id3, _ := mustCreate(t, st, ctx, "r", 3)

	// Fourth should be rejected.
	n := Notification{
		NotificationID: "ntf_full",
		TokenID:        "bootstrap", ReceiverID: "r",
		ContentHash: "h", Title: "t", Body: "b",
		Priority: "normal", DataJSON: "{}", TTLSeconds: 3600,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	out, _, _, err := st.CreateNotification(ctx, n, time.Hour, 3)
	if err != nil {
		t.Fatal(err)
	}
	if out != OutcomeBacklogFull {
		t.Fatalf("expected OutcomeBacklogFull, got %v", out)
	}

	// Mark one as sent → frees capacity (sent does not count).
	if err := st.MarkSent(ctx, id3); err != nil {
		t.Fatal(err)
	}
	n.NotificationID = "ntf_after_free"
	out, _, _, err = st.CreateNotification(ctx, n, time.Hour, 3)
	if err != nil || out != OutcomeCreated {
		t.Fatalf("expected created after freeing capacity: out=%v err=%v", out, err)
	}
}

// TestSweepExpiredStates verifies that SweepExpired transitions
// accepted/queued/sent rows to expired (not dropped).
func TestSweepExpiredStates(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	// All three start as accepted.
	_, _ = mustCreate(t, st, ctx, "r", 0) // stays accepted
	id2, _ := mustCreate(t, st, ctx, "r", 0)
	id3, _ := mustCreate(t, st, ctx, "r", 0)

	// Put them in different pre-expiry states.
	_ = st.MarkSent(ctx, id2)
	_ = st.MarkQueued(ctx, id3)

	// Now create a non-expired one with a long TTL to verify it survives the
	// sweep. The first three have a 1-hour TTL; the sweep cutoff below is 2 hours
	// in the future, so only the first three are expired relative to it.
	longN := Notification{
		NotificationID: "ntf_long_ttl",
		TokenID:        "bootstrap", ReceiverID: "r",
		ContentHash: "h", Title: "t", Body: "b",
		Priority: "normal", DataJSON: "{}", TTLSeconds: 86400,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if out, _, _, err := st.CreateNotification(ctx, longN, time.Hour, 0); err != nil || out != OutcomeCreated {
		t.Fatalf("create long-ttl: out=%v err=%v", out, err)
	}

	// Sweep: the first three (1h TTL) are expired relative to +2h.
	affected, err := st.SweepExpired(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if affected != 3 {
		t.Fatalf("sweep affected %d, want 3", affected)
	}

	// Verify id1 is now expired via the replay set (it should be excluded).
	replay, err := st.ReplaySet(ctx, "r", 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range replay {
		if n.Status != "queued" && n.Status != "sent" {
			t.Errorf("unexpected status %q in replay set", n.Status)
		}
	}
	// id1 was accepted → expired. id2 was sent → expired. id3 was queued → expired.
	// The 4th notification (accepted, not expired yet) should NOT appear in replay
	// because accepted is not in the replay set.
	if len(replay) != 0 {
		t.Fatalf("expected 0 replayable rows after sweep, got %d", len(replay))
	}
}

// TestDeleteAckedOlder verifies the 24-hour hard retention on acked rows.
func TestDeleteAckedOlder(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	id1, e1 := mustCreate(t, st, ctx, "r", 0)
	id2, _ := mustCreate(t, st, ctx, "r", 0)

	// Ack id1.
	_ = st.MarkSent(ctx, id1)
	_, _ = st.ApplyAck(ctx, "r", e1)

	// Delete acked rows created before 1 hour ago: nothing (rows are fresh).
	n, err := st.DeleteAckedOlder(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deletions for fresh rows, got %d", n)
	}

	// Use a future cutoff so all acked rows are "old" relative to it.
	n, err = st.DeleteAckedOlder(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deletion, got %d", n)
	}

	// id2 (not acked) should still exist.
	count, err := st.BacklogCount(ctx, "r", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// id2 is accepted, not in backlog count (queued/sent). But it should still
	// be in the DB. Verify via replay set after marking it sent.
	_ = st.MarkSent(ctx, id2)
	count, _ = st.BacklogCount(ctx, "r", time.Now())
	if count != 1 {
		t.Fatalf("id2 should survive deletion: backlog count %d, want 1", count)
	}
}
