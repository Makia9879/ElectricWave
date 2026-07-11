package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/makia9879/electricwave/internal/auth"
	"github.com/makia9879/electricwave/internal/idgen"
	"github.com/makia9879/electricwave/internal/store"
)

func TestStreamHeartbeat(t *testing.T) {
	app, _, _ := newTestApp(t, 80*time.Millisecond)
	srv := newServer(t, app)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()

	// Expect a heartbeat comment within ~heartbeat interval.
	got := false
deadline:
	for start := time.Now(); time.Since(start) < 2*time.Second; {
		select {
		case f := <-frames:
			if f.comment == ": heartbeat" {
				got = true
				break deadline
			}
		case <-time.After(2 * time.Second):
			break deadline
		}
	}
	if !got {
		t.Fatal("did not receive heartbeat within deadline")
	}
}

func TestStreamAuthErrors(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// Unknown receiver -> 404.
	resp := getStream(t, srv, "ghost", testIdentityTok)
	expectStatus(t, resp, 404)

	// Disabled receiver -> 403.
	resp = getStream(t, srv, "phone-disabled", testIdentityTok)
	expectStatus(t, resp, 403)

	// Wrong identity token -> 401.
	resp = getStream(t, srv, testReceiver, "wrong-identity")
	expectStatus(t, resp, 401)
}

func TestStreamResponseHeaders(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)
	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/receivers/"+testReceiver+"/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+testIdentityTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type wrong: %s", ct)
	}
	for _, h := range []string{"Cache-Control", "Connection", "X-Accel-Buffering"} {
		if resp.Header.Get(h) == "" {
			t.Fatalf("missing header %s", h)
		}
	}
	_ = frames
}

func TestStreamNewConnectionReplacesOld(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	conn1, _ := openStream(t, srv, testReceiver, testIdentityTok)
	// Open a second connection for the same receiver.
	_, closeConn2 := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeConn2()

	// conn1 should close (its readSSE goroutine returns -> channel closes).
	closed := false
deadline:
	for start := time.Now(); time.Since(start) < 2*time.Second; {
		select {
		case _, ok := <-conn1:
			if !ok {
				closed = true
				break deadline
			}
		case <-time.After(2 * time.Second):
			break deadline
		}
	}
	if !closed {
		t.Fatal("old connection was not replaced/closed")
	}
}

func TestStreamDisconnectRemovesOnline(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)
	_, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	time.Sleep(80 * time.Millisecond)
	closeStream()
	// Give the server a moment to observe the closed connection.
	time.Sleep(120 * time.Millisecond)

	// Now a webhook should see the receiver as offline; it gets queued (202).
	resp := postJSON(t, srv, testWebhookTok, "application/json", validBody())
	expectStatus(t, resp, 202)
}

func TestTestEndpoint(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// Offline -> 503.
	resp := postTest(t, srv, testReceiver, testIdentityTok)
	expectStatus(t, resp, 503)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()
	time.Sleep(80 * time.Millisecond)

	// Online -> 200 and a delivered test event.
	resp = postTest(t, srv, testReceiver, testIdentityTok)
	expectStatus(t, resp, 200)
	var got struct {
		Status         string `json:"status"`
		NotificationID string `json:"notification_id"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Status != "test_sent" || !strings.HasPrefix(got.NotificationID, "ntf_") {
		t.Fatalf("test response wrong: %+v", got)
	}

	// Receive the test notification event (skip the initial info event).
	f := nextNotificationFrame(t, frames)
	var ev map[string]any
	json.Unmarshal([]byte(f.data), &ev)
	if ev["title"] != "测试通知" || ev["body"] != "这是一条来自服务端的测试通知" {
		t.Fatalf("test event payload wrong: %+v", ev)
	}
	var d map[string]any
	_ = json.Unmarshal([]byte(toJSONString(ev["data"])), &d)
	if d["test"] != true {
		t.Fatalf("test data wrong: %+v", ev["data"])
	}
}

func TestTestEndpointAuthErrors(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)
	resp := postTest(t, srv, "ghost", testIdentityTok)
	expectStatus(t, resp, 404)
	resp = postTest(t, srv, testReceiver, "wrong")
	expectStatus(t, resp, 401)
}

// TestNoSecretsInLogs verifies the structured log output never contains the
// raw webhook token, the receiver identity token, the Authorization header
// name, or the notification body text.
func TestNoSecretsInLogs(t *testing.T) {
	app, logBuf, st := newTestApp(t, time.Second)
	srv := newServer(t, app)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()
	time.Sleep(80 * time.Millisecond)

	// Successful notification with a distinctive body.
	resp := postJSON(t, srv, testWebhookTok, "application/json", validBody())
	expectStatus(t, resp, 201)
	resp.Body.Close()
	<-frames

	// A couple of error paths to exercise their logging.
	resp = postJSON(t, srv, "wrong", "application/json", validBody())
	resp.Body.Close()
	resp = postJSON(t, srv, testWebhookTok, "application/json", `{"receiver_id":"ghost","title":"t","body":"b"}`)
	resp.Body.Close()

	logOut := logBuf.String()
	forbidden := []string{
		testWebhookTok,
		testIdentityTok,
		"Authorization",
		bodyCanary,
	}
	for _, f := range forbidden {
		if strings.Contains(logOut, f) {
			t.Errorf("log contains forbidden substring %q:\n%s", f, logOut)
		}
	}

	// The persisted audit rows must likewise not contain raw secrets.
	rows, err := st.EnabledWebhookTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range rows {
		if tk.Hash == testWebhookTok {
			t.Error("stored webhook hash equals raw token (hashing broken)")
		}
	}
	recv, err := st.GetReceiver(context.Background(), testReceiver)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Verify(testIdentityTok, recv.IdentityTokenHash, "") {
		t.Fatal("identity token hash mismatch")
	}
	// Sanity: stored hash is not the raw token.
	if recv.IdentityTokenHash == testIdentityTok {
		t.Error("stored identity hash equals raw token")
	}
}

// --- small request helpers --------------------------------------------------

func getStream(t *testing.T, srv *httptest.Server, receiverID, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/receivers/"+receiverID+"/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func postTest(t *testing.T, srv *httptest.Server, receiverID, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/receivers/"+receiverID+"/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func toJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// --- reliable reconnect & replay tests (0007-integration-contract §4) ---

// queueOffline posts a notification while the receiver is offline and returns
// the decoded 202 queued response fields.
func queueOffline(t *testing.T, srv *httptest.Server, body string) struct {
	EventID int64 `json:"event_id"`
} {
	t.Helper()
	resp := postJSON(t, srv, testWebhookTok, "application/json", body)
	expectStatus(t, resp, 202)
	var got struct {
		EventID int64 `json:"event_id"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	return got
}

// TestInfoEventOnConnect verifies the info control event is the first frame and
// carries the correct backlog summary fields.
func TestInfoEventOnConnect(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// Queue two notifications while offline.
	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"a","body":"a"}`)
	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"b","body":"b"}`)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()

	f := nextFrame(t, frames)
	if f.event != "info" {
		t.Fatalf("first frame must be info, got %+v", f)
	}
	var info struct {
		Type                    string  `json:"type"`
		AckedEventID            *int64  `json:"acked_event_id"`
		OldestUnackedEventID    *int64  `json:"oldest_unacked_event_id"`
		NewestEventID           *int64  `json:"newest_event_id"`
		BacklogCount            int     `json:"backlog_count"`
		OldestUnackedAcceptedAt *string `json:"oldest_unacked_accepted_at"`
	}
	json.Unmarshal([]byte(f.data), &info)
	if info.BacklogCount != 2 {
		t.Fatalf("backlog_count: got %d, want 2", info.BacklogCount)
	}
	if info.OldestUnackedEventID == nil || *info.OldestUnackedEventID != 1 {
		t.Fatalf("oldest_unacked_event_id: got %v, want 1", info.OldestUnackedEventID)
	}
	if info.NewestEventID == nil || *info.NewestEventID != 2 {
		t.Fatalf("newest_event_id: got %v, want 2", info.NewestEventID)
	}
	if info.AckedEventID != nil {
		t.Fatalf("acked_event_id should be null on first connect, got %v", *info.AckedEventID)
	}
	// §10.4 "最老积压时间": with 2 queued items the oldest must carry a timestamp.
	if info.OldestUnackedAcceptedAt == nil || *info.OldestUnackedAcceptedAt == "" {
		t.Fatalf("oldest_unacked_accepted_at should be set with pending backlog, got %v", info.OldestUnackedAcceptedAt)
	}
}

// TestReplayOrder verifies that queued notifications are replayed in ascending
// event_id order after the info event.
func TestReplayOrder(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"1","body":"1"}`)
	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"2","body":"2"}`)
	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"3","body":"3"}`)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()

	// Skip info, then expect three notification events in order.
	_ = nextFrame(t, frames) // info
	for i := int64(1); i <= 3; i++ {
		f := nextNotificationFrame(t, frames)
		if f.id != strconv.FormatInt(i, 10) {
			t.Fatalf("replay order: event %d got id %q", i, f.id)
		}
		var ev map[string]any
		json.Unmarshal([]byte(f.data), &ev)
		if ev["event_id"].(float64) != float64(i) {
			t.Fatalf("event_id mismatch: got %v, want %d", ev["event_id"], i)
		}
	}
}

// TestLastEventIDCursor verifies that Last-Event-ID limits the replay set.
func TestLastEventIDCursor(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// Queue 5 notifications.
	for i := 1; i <= 5; i++ {
		queueOffline(t, srv, `{"receiver_id":"phone-main","title":"t","body":"b"}`)
	}

	frames, closeStream := openStreamWithHeaders(t, srv, testReceiver, testIdentityTok,
		map[string]string{"Last-Event-ID": "2"})
	defer closeStream()

	_ = nextFrame(t, frames) // info

	// Only events 3, 4, 5 should be replayed.
	for i := int64(3); i <= 5; i++ {
		f := nextNotificationFrame(t, frames)
		if f.id != strconv.FormatInt(i, 10) {
			t.Fatalf("cursor replay: expected event %d, got id %q", i, f.id)
		}
	}
}

// TestReceiverAckCleanup verifies that X-Receiver-Ack transitions queued/sent
// rows to acked and that the info event reflects the updated state.
func TestReceiverAckCleanup(t *testing.T) {
	app, _, st := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// Queue 3 notifications.
	for i := 1; i <= 3; i++ {
		queueOffline(t, srv, `{"receiver_id":"phone-main","title":"t","body":"b"}`)
	}

	frames, closeStream := openStreamWithHeaders(t, srv, testReceiver, testIdentityTok,
		map[string]string{"X-Receiver-Ack": "2"})
	defer closeStream()

	f := nextFrame(t, frames)
	if f.event != "info" {
		t.Fatalf("first frame must be info, got %+v", f)
	}
	var info struct {
		AckedEventID *int64 `json:"acked_event_id"`
		BacklogCount int    `json:"backlog_count"`
	}
	json.Unmarshal([]byte(f.data), &info)
	if info.AckedEventID == nil || *info.AckedEventID != 2 {
		t.Fatalf("acked_event_id: got %v, want 2", info.AckedEventID)
	}
	if info.BacklogCount != 1 {
		t.Fatalf("backlog_count after ack: got %d, want 1", info.BacklogCount)
	}

	// Only event 3 should be replayed.
	f2 := nextNotificationFrame(t, frames)
	if f2.id != "3" {
		t.Fatalf("post-ack replay: expected event 3, got id %q", f2.id)
	}

	// Verify the acked rows are persisted: events 1-2 are acked, event 3 is
	// sent (replayed). BacklogCount counts sent rows, so 1.
	count, err := st.BacklogCount(context.Background(), testReceiver, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("post-ack backlog count in DB: got %d, want 1 (event 3 replayed to sent)", count)
	}
}

// TestReceiverAckIdempotent verifies that re-acking an old value on a
// subsequent reconnect does not error and the acked count stays consistent.
func TestReceiverAckIdempotent(t *testing.T) {
	app, _, st := newTestApp(t, time.Second)
	srv := newServer(t, app)

	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"t","body":"b"}`)
	queueOffline(t, srv, `{"receiver_id":"phone-main","title":"t2","body":"b2"}`)

	// First connection: ack 1.
	frames1, close1 := openStreamWithHeaders(t, srv, testReceiver, testIdentityTok,
		map[string]string{"X-Receiver-Ack": "1"})
	f := nextFrame(t, frames1)
	if f.event != "info" {
		t.Fatalf("expected info, got %+v", f)
	}
	close1()

	// Second connection: re-ack 1 (old value). Must not error.
	frames2, close2 := openStreamWithHeaders(t, srv, testReceiver, testIdentityTok,
		map[string]string{"X-Receiver-Ack": "1"})
	defer close2()
	f = nextFrame(t, frames2)
	if f.event != "info" {
		t.Fatalf("expected info on re-ack, got %+v", f)
	}
	var info struct {
		AckedEventID *int64 `json:"acked_event_id"`
		BacklogCount int    `json:"backlog_count"`
	}
	json.Unmarshal([]byte(f.data), &info)
	if info.AckedEventID == nil || *info.AckedEventID != 1 {
		t.Fatalf("re-ack acked_event_id: got %v, want 1", info.AckedEventID)
	}
	// Event 2 should still be replayable.
	if info.BacklogCount != 1 {
		t.Fatalf("re-ack backlog count: got %d, want 1", info.BacklogCount)
	}
	// DB state: exactly 1 acked, 1 queued.
	count, _ := st.BacklogCount(context.Background(), testReceiver, time.Now())
	if count != 1 {
		t.Fatalf("DB backlog count after re-ack: got %d, want 1", count)
	}
}

// TestBacklogGap verifies that a gap in the replay window triggers a
// backlog_gap control event.
func TestBacklogGap(t *testing.T) {
	app, _, st := newTestApp(t, time.Second)
	srv := newServer(t, app)

	ctx := context.Background()
	// Seed two short-TTL notifications and one long-TTL notification directly
	// through the store, then mark them queued so they appear in the replay /
	// gap queries. After a sweep, events 1 and 2 (short TTL) are expired,
	// leaving a gap when the client cursor is 1 and the oldest remaining
	// unacked is 3.
	var ids []string
	for _, ttl := range []time.Duration{60 * time.Second, 60 * time.Second, 3600 * time.Second} {
		n := store.Notification{
			NotificationID: idgen.NotificationID(),
			TokenID:        "bootstrap", ReceiverID: testReceiver,
			ContentHash: "h", Title: "t", Body: "b",
			Priority: "normal", DataJSON: "{}",
			TTLSeconds: int(ttl.Seconds()),
			ExpiresAt:  time.Now().Add(ttl),
		}
		out, id, _, err := st.CreateNotification(ctx, n, time.Hour, 0)
		if err != nil || out != store.OutcomeCreated {
			t.Fatalf("create: out=%v err=%v", out, err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := st.MarkQueued(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	// Sweep with a cutoff 2 minutes in the future: the two 60s-TTL rows expire.
	affected, err := st.SweepExpired(ctx, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if affected != 2 {
		t.Fatalf("sweep affected %d, want 2", affected)
	}

	// Connect with Last-Event-ID=0. The oldest unacked is event 3; cursor=0 so
	// cursor < 1 → no gap. Instead connect with Last-Event-ID=1: cursor+1=2 < 3.
	frames, closeStream := openStreamWithHeaders(t, srv, testReceiver, testIdentityTok,
		map[string]string{"Last-Event-ID": "1"})
	defer closeStream()

	// Expect: info, backlog_gap, notification(3).
	fInfo := nextFrame(t, frames)
	if fInfo.event != "info" {
		t.Fatalf("expected info, got %+v", fInfo)
	}

	fGap := nextFrame(t, frames)
	if fGap.event != "backlog_gap" {
		t.Fatalf("expected backlog_gap, got %+v", fGap)
	}
	var gap struct {
		FromEventID int64  `json:"from_event_id"`
		ToEventID   int64  `json:"to_event_id"`
		Reason      string `json:"reason"`
	}
	json.Unmarshal([]byte(fGap.data), &gap)
	if gap.FromEventID != 2 || gap.ToEventID != 2 {
		t.Fatalf("gap range: got %d..%d, want 2..2", gap.FromEventID, gap.ToEventID)
	}
	if gap.Reason != "retention_exceeded" {
		t.Fatalf("gap reason: got %q, want retention_exceeded", gap.Reason)
	}

	// Then event 3 should be replayed.
	f3 := nextNotificationFrame(t, frames)
	if f3.id != "3" {
		t.Fatalf("expected replay of event 3, got id %q", f3.id)
	}
}

// TestRealtimeEventHasID verifies that a real-time notification (delivered via
// the hub after handshake) includes the id: line matching event_id.
func TestRealtimeEventHasID(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()
	time.Sleep(80 * time.Millisecond)

	resp := postJSON(t, srv, testWebhookTok, "application/json", validBody())
	expectStatus(t, resp, 201)
	var created struct {
		EventID int64 `json:"event_id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	f := nextNotificationFrame(t, frames)
	if f.id != strconv.FormatInt(created.EventID, 10) {
		t.Fatalf("real-time id: line: got %q, want %d", f.id, created.EventID)
	}
	var ev map[string]any
	json.Unmarshal([]byte(f.data), &ev)
	if ev["event_id"].(float64) != float64(created.EventID) {
		t.Fatalf("real-time JSON event_id: got %v, want %d", ev["event_id"], created.EventID)
	}
}
