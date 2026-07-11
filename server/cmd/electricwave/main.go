// Command electricwave runs the ElectricWave notification HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/makia9879/electricwave/internal/api"
	"github.com/makia9879/electricwave/internal/auth"
	"github.com/makia9879/electricwave/internal/config"
	"github.com/makia9879/electricwave/internal/hub"
	"github.com/makia9879/electricwave/internal/logging"
	"github.com/makia9879/electricwave/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("fatal", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}
	log := logging.New()

	st, err := store.Open(cfg.StoragePath)
	if err != nil {
		return err
	}
	defer st.Close()

	// Seed the bootstrap webhook token and receiver from local configuration.
	ctx := context.Background()
	webhookHash := auth.HashToken(cfg.BootstrapWebhookAccessToken, cfg.TokenHashPepper)
	if err := st.UpsertWebhookToken(ctx, "bootstrap", webhookHash, true); err != nil {
		return errors.Join(errors.New("seed webhook token"), err)
	}
	identityHash := auth.HashToken(cfg.BootstrapReceiverIdentityToken, cfg.TokenHashPepper)
	if err := st.UpsertReceiver(ctx, store.Receiver{
		ReceiverID:        cfg.BootstrapReceiverID,
		IdentityTokenHash: identityHash,
		Allowlisted:       cfg.BootstrapReceiverAllowlisted,
		Enabled:           cfg.BootstrapReceiverEnabled,
		ProviderType:      cfg.DeliveryProvider,
	}); err != nil {
		return errors.Join(errors.New("seed receiver"), err)
	}
	log.Info("bootstrap complete", "receiver_id", cfg.BootstrapReceiverID, "token_id", "bootstrap")

	h := hub.New()
	app, err := api.New(cfg, log, st, h)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0, // SSE connections are long-lived
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	// Best-effort TTL housekeeping.
	stopSweeper := startTTLSweeper(ctx, st, log)
	defer stopSweeper()

	// Evict idle rate-limit buckets so the per-key limiter maps stay bounded.
	stopRLSweeper := startRateLimitSweeper(ctx, app)
	defer stopRLSweeper()

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(errors.New("shutdown"), err)
	}
	log.Info("http server stopped")
	return nil
}

// startTTLSweeper periodically transitions expired notifications to the
// expired state and deletes acked rows older than 24 hours (§6). It returns a
// stop function.
func startTTLSweeper(ctx context.Context, st *store.Store, log *slog.Logger) func() {
	ticker := time.NewTicker(10 * time.Minute)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				now := time.Now()
				if n, err := st.SweepExpired(ctx, now); err != nil {
					log.Warn("ttl sweep failed", "err", err.Error())
				} else if n > 0 {
					log.Info("ttl sweep", "expired", n)
				}
				cutoff := now.Add(-24 * time.Hour)
				if n, err := st.DeleteAckedOlder(ctx, cutoff); err != nil {
					log.Warn("acked retention sweep failed", "err", err.Error())
				} else if n > 0 {
					log.Info("acked retention sweep", "deleted", n)
				}
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
	}
}

// startRateLimitSweeper periodically evicts idle rate-limit buckets to bound
// memory. It returns a stop function.
func startRateLimitSweeper(ctx context.Context, app *api.App) func() {
	ticker := time.NewTicker(10 * time.Minute)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				app.SweepRateLimits()
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
	}
}
