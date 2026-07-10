package store

import (
	"context"
	"path/filepath"
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

func TestCreateNotificationIdempotency(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	n := Notification{
		NotificationID: "ntf_1", TokenID: "bootstrap", ReceiverID: "r",
		IdempotencyKey: "k", ContentHash: "h1", Title: "t", Body: "b",
		Priority: "normal", DataJSON: "{}", TTLSeconds: 3600,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	out, id, err := st.CreateNotification(ctx, n, time.Hour)
	if err != nil || out != OutcomeCreated || id != "ntf_1" {
		t.Fatalf("first create: out=%v id=%s err=%v", out, id, err)
	}

	// Same key, same content -> duplicate, returns original id.
	n.NotificationID = "ntf_2"
	out, id, err = st.CreateNotification(ctx, n, time.Hour)
	if err != nil || out != OutcomeDuplicate || id != "ntf_1" {
		t.Fatalf("duplicate: out=%v id=%s err=%v", out, id, err)
	}

	// Same key, different content -> conflict.
	n.ContentHash = "h2"
	n.NotificationID = "ntf_3"
	out, id, err = st.CreateNotification(ctx, n, time.Hour)
	if err != nil || out != OutcomeConflict {
		t.Fatalf("conflict: out=%v id=%s err=%v", out, id, err)
	}

	// Empty idempotency key always creates.
	n.IdempotencyKey = ""
	n.NotificationID = "ntf_4"
	out, id, err = st.CreateNotification(ctx, n, time.Hour)
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
	if _, _, err := st.CreateNotification(ctx, n, time.Hour); err != nil {
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
	if _, _, err := st.CreateNotification(ctx, n, time.Hour); err != nil {
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
