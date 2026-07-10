package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/makia9879/makia-notice/internal/auth"
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

	// Now a webhook should see the receiver as offline (503).
	resp := postJSON(t, srv, testWebhookTok, "application/json", validBody())
	expectStatus(t, resp, 503)
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

	select {
	case f := <-frames:
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
	case <-time.After(2 * time.Second):
		t.Fatal("test event not received")
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
