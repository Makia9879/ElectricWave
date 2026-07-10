package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/makia9879/makia-notice/internal/auth"
	"github.com/makia9879/makia-notice/internal/config"
	"github.com/makia9879/makia-notice/internal/hub"
	"github.com/makia9879/makia-notice/internal/idgen"
	"github.com/makia9879/makia-notice/internal/ratelimit"
	"github.com/makia9879/makia-notice/internal/store"
)

// idempotencyWindow is the retention window for duplicate detection.
const idempotencyWindow = 24 * time.Hour

// App holds all collaborators required to serve the HTTP API.
type App struct {
	cfg             *config.Config
	log             *slog.Logger
	store           *store.Store
	checker         *auth.Checker
	hub             *hub.Hub
	ipLimiter       *ratelimit.KeyedLimiter
	tokenLimiter    *ratelimit.KeyedLimiter
	receiverLimiter *ratelimit.KeyedLimiter
}

// New wires the App from its collaborators and seeds the in-memory auth
// checker from the database.
func New(cfg *config.Config, log *slog.Logger, st *store.Store, h *hub.Hub) (*App, error) {
	tokens, err := st.EnabledWebhookTokens(context.Background())
	if err != nil {
		return nil, err
	}
	return &App{
		cfg:             cfg,
		log:             log,
		store:           st,
		checker:         auth.NewChecker(tokens, cfg.TokenHashPepper),
		hub:             h,
		ipLimiter:       ratelimit.New(cfg.RatePerIPPerMin),
		tokenLimiter:    ratelimit.New(cfg.RatePerTokenPerMin),
		receiverLimiter: ratelimit.New(cfg.RatePerReceiverPerMin),
	}, nil
}

// Handler builds the HTTP handler with middleware and the route table.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("POST /api/v1/notifications", a.handleNotifications)
	mux.HandleFunc("GET /api/v1/receivers/{receiver_id}/stream", a.handleStream)
	mux.HandleFunc("POST /api/v1/receivers/{receiver_id}/test", a.handleTest)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := idgen.RequestID()
		w.Header().Set("X-Request-ID", rid)
		ai := &auditInfo{requestID: rid, start: time.Now()}
		r = r.WithContext(context.WithValue(r.Context(), auditCtxKey{}, ai))
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if rcv := recover(); rcv != nil {
				a.log.Error("panic recovered",
					"request_id", rid,
					"path", r.URL.Path,
					"panic", fmt.Sprintf("%v", rcv),
				)
				setAudit(r, auditFields{statusCode: http.StatusInternalServerError, errorClass: CodeInternalError})
				if !rec.written {
					a.writeError(rec, r, errInternal())
				}
			}
			a.finalizeAudit(r, rec)
		}()

		mux.ServeHTTP(rec, r)
	})
}

// --- audit context -------------------------------------------------------

type auditCtxKey struct{}

type auditInfo struct {
	requestID        string
	tokenID          string
	receiverID       string
	provider         string
	statusCode       int
	errorClassStored string
	start            time.Time
}

type auditFields struct {
	statusCode int
	errorClass string
	tokenID    string
	receiverID string
	provider   string
}

func auditFrom(r *http.Request) *auditInfo {
	if v, ok := r.Context().Value(auditCtxKey{}).(*auditInfo); ok && v != nil {
		return v
	}
	return &auditInfo{start: time.Now()}
}

func setAudit(r *http.Request, f auditFields) {
	ai := auditFrom(r)
	if f.statusCode != 0 {
		ai.statusCode = f.statusCode
	}
	if f.errorClass != "" {
		ai.errorClassStored = f.errorClass
	}
	if f.tokenID != "" {
		ai.tokenID = f.tokenID
	}
	if f.receiverID != "" {
		ai.receiverID = f.receiverID
	}
	if f.provider != "" {
		ai.provider = f.provider
	}
}

// finalizeAudit writes the audit row and a structured access log line. It never
// logs the Authorization header, tokens or body content.
func (a *App) finalizeAudit(r *http.Request, rec *recorder) {
	// /healthz is a container health probe, not a business request: skip the
	// audit row and access log so probes don't flood audit storage and logs.
	if r.URL.Path == "/healthz" {
		return
	}
	ai := auditFrom(r)
	status := ai.statusCode
	if status == 0 {
		status = rec.status
	}
	dur := time.Since(ai.start)
	errClass := ai.errorClassStored
	if errClass == "" && status >= 400 {
		errClass = statusClass(status)
	}

	a.log.Info("http request",
		"request_id", ai.requestID,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", dur.Milliseconds(),
		"token_id", ai.tokenID,
		"receiver_id", ai.receiverID,
		"provider", ai.provider,
		"remote_ip", a.clientIP(r),
	)

	aud := store.Audit{
		RequestID:  ai.requestID,
		TokenID:    ai.tokenID,
		ReceiverID: ai.receiverID,
		Provider:   ai.provider,
		StatusCode: status,
		ErrorClass: errClass,
		DurationMS: dur.Milliseconds(),
		CreatedAt:  time.Now().UTC(),
	}
	// Use a detached context: for SSE the request context is already cancelled
	// by the time the handler returns (client disconnected), which would drop
	// the audit row.
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.store.InsertAudit(auditCtx, aud); err != nil {
		a.log.Warn("audit write failed", "request_id", ai.requestID, "err", err.Error())
	}
}

// SweepRateLimits evicts idle per-key rate-limit buckets so the in-memory maps
// don't grow without bound. Called periodically by the housekeeping goroutine.
func (a *App) SweepRateLimits() {
	now := time.Now()
	a.ipLimiter.Cleanup(now)
	a.tokenLimiter.Cleanup(now)
	a.receiverLimiter.Cleanup(now)
}

// --- helpers -------------------------------------------------------------

// clientIP resolves the caller IP honoring trusted-proxy headers only when the
// direct peer is in TRUSTED_PROXY_ADDRS.
func (a *App) clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	if _, ok := a.cfg.TrustedProxyAddrs[host]; ok {
		// Trust only X-Real-IP from the trusted proxy (nginx overwrites it with
		// the real TCP peer). X-Forwarded-For is intentionally ignored: its
		// leftmost entry is client-controlled and its rightmost is already what
		// nginx writes into X-Real-IP.
		if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
			if net.ParseIP(strings.TrimSpace(xrip)) != nil {
				return strings.TrimSpace(xrip)
			}
		}
	}
	return host
}

// bearerToken extracts a Bearer token from the Authorization header without
// ever logging the value.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// isJSONContentType reports whether the media type is application/json
// (parameters such as charset are ignored).
func isJSONContentType(h string) bool {
	if i := strings.IndexByte(h, ';'); i >= 0 {
		h = h[:i]
	}
	return strings.EqualFold(strings.TrimSpace(h), "application/json")
}

func statusClass(status int) string {
	switch status {
	case 429:
		return CodeRateLimited
	case 401:
		return CodeUnauthorized
	case 403:
		return CodeReceiverNotAllowed
	case 404:
		return CodeReceiverNotFound
	case 409:
		return CodeIdempotencyConflict
	case 413:
		return CodePayloadTooLarge
	case 415:
		return CodeUnsupportedMediaType
	case 503:
		return CodeDeliveryUnavailable
	}
	switch {
	case status >= 500:
		return CodeInternalError
	case status >= 400:
		return CodeInvalidRequest
	}
	return ""
}

// recorder captures the response status and proxies Flush for SSE.
type recorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *recorder) WriteHeader(s int) {
	if !r.written {
		r.status = s
		r.written = true
	}
	r.ResponseWriter.WriteHeader(s)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.written {
		r.status = http.StatusOK
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
