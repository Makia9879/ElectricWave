// Package config loads runtime configuration from a local .env file (optional)
// and the process environment. A missing .env file is not an error; required
// secrets must still be provided through the environment.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	HTTPAddr                    string
	PublicBaseURL               string
	DeliveryProvider            string
	SSEHeartbeatInterval        time.Duration
	TrustedProxyAddrs           map[string]struct{}

	BootstrapWebhookAccessToken      string
	BootstrapReceiverID              string
	BootstrapReceiverIdentityToken   string
	BootstrapReceiverEnabled         bool
	BootstrapReceiverAllowlisted     bool

	StoragePath string
	TokenHashPepper string

	RatePerTokenPerMin     int
	RatePerIPPerMin        int
	RatePerReceiverPerMin  int
}

// Load reads the optional .env file at path (ignoring "file not found") and
// then resolves configuration from the merged environment.
func Load(path string) (*Config, error) {
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if err := loadDotEnv(path); err != nil {
				return nil, fmt.Errorf("load %s: %w", path, err)
			}
		}
	}

	cfg := &Config{
		HTTPAddr:                 getEnv("HTTP_ADDR", ":8788"),
		PublicBaseURL:            getEnv("PUBLIC_BASE_URL", "https://notice.makia98.com"),
		DeliveryProvider:         getEnv("DELIVERY_PROVIDER", "self_hosted_sse"),
		SSEHeartbeatInterval:     time.Duration(getEnvInt("SSE_HEARTBEAT_INTERVAL_SECONDS", 30)) * time.Second,
		BootstrapReceiverID:      getEnv("BOOTSTRAP_RECEIVER_ID", "phone-main"),
		BootstrapReceiverEnabled: getEnvBool("BOOTSTRAP_RECEIVER_ENABLED", true),
		BootstrapReceiverAllowlisted: getEnvBool("BOOTSTRAP_RECEIVER_ALLOWLISTED", true),
		StoragePath:              getEnv("STORAGE_PATH", "./data/notice.db"),
		TokenHashPepper:          os.Getenv("TOKEN_HASH_PEPPER"),
		RatePerTokenPerMin:       getEnvInt("RATE_LIMIT_PER_TOKEN_PER_MIN", 60),
		RatePerIPPerMin:          getEnvInt("RATE_LIMIT_PER_IP_PER_MIN", 120),
		RatePerReceiverPerMin:    getEnvInt("RATE_LIMIT_PER_RECEIVER_PER_MIN", 60),
	}
	cfg.TrustedProxyAddrs = parseSet(getEnv("TRUSTED_PROXY_ADDRS", "127.0.0.1,::1"))

	// Normalize a zero heartbeat to the spec default.
	if cfg.SSEHeartbeatInterval <= 0 {
		cfg.SSEHeartbeatInterval = 30 * time.Second
	}

	cfg.BootstrapWebhookAccessToken = os.Getenv("BOOTSTRAP_WEBHOOK_ACCESS_TOKEN")
	cfg.BootstrapReceiverIdentityToken = os.Getenv("BOOTSTRAP_RECEIVER_IDENTITY_TOKEN")

	if cfg.BootstrapWebhookAccessToken == "" {
		return nil, fmt.Errorf("BOOTSTRAP_WEBHOOK_ACCESS_TOKEN is required but empty")
	}
	if cfg.BootstrapReceiverIdentityToken == "" {
		return nil, fmt.Errorf("BOOTSTRAP_RECEIVER_IDENTITY_TOKEN is required but empty")
	}
	if cfg.BootstrapReceiverID == "" {
		return nil, fmt.Errorf("BOOTSTRAP_RECEIVER_ID must not be empty")
	}
	return cfg, nil
}

// loadDotEnv parses a simple KEY=VALUE file into the process environment.
// Existing environment variables take precedence (file values do not overwrite
// real env vars), mirroring godotenv's Override=false behaviour.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = unquote(val)
		if _, ok := os.LookupEnv(key); !ok {
			_ = os.Setenv(key, val)
		}
	}
	return sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func getEnvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func parseSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}
