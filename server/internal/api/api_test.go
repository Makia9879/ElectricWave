package api

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/makia9879/electricwave/internal/auth"
	"github.com/makia9879/electricwave/internal/config"
	"github.com/makia9879/electricwave/internal/hub"
	"github.com/makia9879/electricwave/internal/store"
)

// lockedBuffer is a concurrency-safe bytes.Buffer so the test can read log
// output while the server writes audit/access lines from request goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

const (
	testWebhookTok  = "test-webhook-secret-0123456789abcdefABCDEF"
	testIdentityTok = "test-identity-secret-987654321fedcbaFEDCBA"
	testReceiver    = "phone-main"
	bodyCanary      = "DISTINCT_BODY_CANARY_订单已支付_123"
)

type sseFrame struct {
	event   string
	data    string
	comment string
	id      string
}

// newTestApp builds an App backed by a temp SQLite DB and a captured log
// buffer so redaction can be asserted. The bootstrap webhook token and
// receiver are seeded directly into the store (no .env needed).
func newTestApp(t *testing.T, heartbeat time.Duration) (*App, *lockedBuffer, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "notice.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	if err := st.UpsertWebhookToken(ctx, "bootstrap", auth.HashToken(testWebhookTok, ""), true); err != nil {
		t.Fatal(err)
	}
	seedReceiver(t, st, "phone-main", true, true)
	seedReceiver(t, st, "phone-disabled", false, true)
	seedReceiver(t, st, "phone-notlisted", true, false)

	var logBuf lockedBuffer
	log := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := &config.Config{
		HTTPAddr:              ":0",
		PublicBaseURL:         "https://notice.example",
		DeliveryProvider:      "self_hosted_sse",
		SSEHeartbeatInterval:  heartbeat,
		TrustedProxyAddrs:     map[string]struct{}{"127.0.0.1": {}, "::1": {}},
		StoragePath:           filepath.Join(dir, "notice.db"),
		TokenHashPepper:       "",
		RatePerTokenPerMin:    100000,
		RatePerIPPerMin:       100000,
		RatePerReceiverPerMin: 100000,
		BacklogMaxPerReceiver: 1000,
	}
	h := hub.New()
	app, err := New(cfg, log, st, h)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return app, &logBuf, st
}

func seedReceiver(t *testing.T, st *store.Store, id string, enabled, allowlisted bool) {
	t.Helper()
	if err := st.UpsertReceiver(context.Background(), store.Receiver{
		ReceiverID:        id,
		IdentityTokenHash: auth.HashToken(testIdentityTok, ""),
		Allowlisted:       allowlisted,
		Enabled:           enabled,
		ProviderType:      "self_hosted_sse",
	}); err != nil {
		t.Fatal(err)
	}
}

func newServer(t *testing.T, app *App) *httptest.Server {
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, srv *httptest.Server, token, ct, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/notifications", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// openStream opens an SSE connection and returns a channel of parsed frames and
// a close function.
func openStream(t *testing.T, srv *httptest.Server, receiverID, token string) (<-chan sseFrame, func()) {
	t.Helper()
	return openStreamWithHeaders(t, srv, receiverID, token, nil)
}

// openStreamWithHeaders is like openStream but allows extra request headers
// (e.g., Last-Event-ID, X-Receiver-Ack).
func openStreamWithHeaders(t *testing.T, srv *httptest.Server, receiverID, token string, headers map[string]string) (<-chan sseFrame, func()) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/receivers/"+receiverID+"/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan sseFrame, 16)
	go readSSE(resp.Body, out)
	return out, func() { _ = resp.Body.Close() }
}

func readSSE(body io.ReadCloser, out chan<- sseFrame) {
	defer close(out)
	br := bufio.NewReader(body)
	var event, data, id string
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, ":"):
				select {
				case out <- sseFrame{comment: line}:
				case <-time.After(2 * time.Second):
				}
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, "id: "):
				id = strings.TrimPrefix(line, "id: ")
			case line == "":
				if event != "" || data != "" {
					select {
					case out <- sseFrame{event: event, data: data, id: id}:
					case <-time.After(2 * time.Second):
					}
				}
				event, data, id = "", "", ""
			}
		}
		if err != nil {
			return
		}
	}
}

func expectStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status: got %d, want %d; body=%s", resp.StatusCode, want, readBody(t, resp))
	}
}

// nextNotificationFrame reads frames until a notification event arrives or the
// deadline passes, skipping info / heartbeat control events.
func nextNotificationFrame(t *testing.T, frames <-chan sseFrame) sseFrame {
	t.Helper()
	deadline := 2 * time.Second
	for start := time.Now(); time.Since(start) < deadline; {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("stream closed before notification event")
			}
			if f.event == "notification" {
				return f
			}
		case <-time.After(deadline):
			t.Fatal("did not receive notification event")
		}
	}
	t.Fatal("did not receive notification event")
	return sseFrame{}
}

// nextFrame reads the next frame of any type (info, backlog_gap, notification,
// heartbeat comment) or fails after the deadline.
func nextFrame(t *testing.T, frames <-chan sseFrame) sseFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("stream closed")
		}
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("no frame received within deadline")
		return sseFrame{}
	}
}

func validBody() string {
	return `{"receiver_id":"` + testReceiver + `","title":"t","body":"` + bodyCanary + `","priority":"normal"}`
}

// --- helpers shared by handlers --------------------------------------------------

func TestHealthz(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, resp, 200)
	if !strings.Contains(readBody(t, resp), `"status":"ok"`) {
		t.Fatal("healthz body wrong")
	}
}

func TestRequestIDHeader(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("X-Request-ID"), "req_") {
		t.Fatalf("missing X-Request-ID: %q", resp.Header.Get("X-Request-ID"))
	}
}
