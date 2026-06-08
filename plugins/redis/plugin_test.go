package redis_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/redis"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return redis.New().Match(schema.ParsedOTelLog{
		ServiceName: "redis",
		Body:        body,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if redis.New().Vendor() != "redis" {
		t.Error("Vendor() should be redis")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	if len(redis.New().Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func TestPlugin_Match_MaxmemoryEviction(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantVal string
	}{
		{
			name:    "maxmemory hit with policy",
			body:    `1:M 15 Jan 2024 10:23:41.123 # maxmemory-policy: allkeys-lru`,
			wantVal: "eviction_active:allkeys-lru",
		},
		{
			name:    "maxmemory hit without extractable policy",
			body:    `1:M 15 Jan 2024 10:23:41.456 # maxmemory limit reached`,
			wantVal: "eviction_active",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeSaturation {
				t.Errorf("EventType = %q, want saturation", ev.EventType)
			}
			if ev.NewValue != tt.wantVal {
				t.Errorf("NewValue = %q, want %q", ev.NewValue, tt.wantVal)
			}
		})
	}
}

func TestPlugin_Match_BgsaveFailed(t *testing.T) {
	body := `1:M 15 Jan 2024 10:23:41.123 # Can't save in background: fork: Cannot allocate memory`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeSaturation {
		t.Errorf("EventType = %q, want saturation", ev.EventType)
	}
	if ev.NewValue != "bgsave_failed" {
		t.Errorf("NewValue = %q, want bgsave_failed", ev.NewValue)
	}
}

func TestPlugin_Match_ReplicaMasterUnreachable(t *testing.T) {
	body := `1:S 15 Jan 2024 10:23:41.456 # Connecting to MASTER 127.0.0.1:6379`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeDependencyFailure {
		t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
	}
}

func TestPlugin_Match_ReplicaSync(t *testing.T) {
	body := `1:S 15 Jan 2024 10:23:42.789 * MASTER <-> REPLICA sync started`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeDependencyFailure {
		t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
	}
	if ev.NewValue != "replica_sync" {
		t.Errorf("NewValue = %q, want replica_sync", ev.NewValue)
	}
}

func TestPlugin_Match_Startup(t *testing.T) {
	tests := []struct {
		body string
	}{
		{`1:M 15 Jan 2024 10:23:41.123 * Ready to accept connections tcp`},
		{`1:M 15 Jan 2024 10:23:41.123 * Ready to accept connections`},
	}
	for _, tt := range tests {
		ev, err := match(t, tt.body)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.body, err)
		}
		if ev == nil {
			t.Fatalf("expected event for %q, got nil", tt.body)
		}
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart", ev.EventType)
		}
		if ev.NewValue != "started" {
			t.Errorf("NewValue = %q, want started", ev.NewValue)
		}
	}
}

func TestPlugin_Match_Shutdown(t *testing.T) {
	tests := []struct {
		body string
	}{
		{`1:M 15 Jan 2024 10:23:41.123 # User requested shutdown...`},
		{`1:M 15 Jan 2024 10:23:41.123 # SIGTERM received, scheduling shutdown...`},
		{`1:M 15 Jan 2024 10:23:41.123 # Redis is now ready to exit, bye bye...`},
	}
	for _, tt := range tests {
		ev, err := match(t, tt.body)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.body, err)
		}
		if ev == nil {
			t.Fatalf("expected event for %q, got nil", tt.body)
		}
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart", ev.EventType)
		}
		if ev.NewValue != "shutdown" {
			t.Errorf("NewValue = %q, want shutdown", ev.NewValue)
		}
	}
}

func TestPlugin_Match_Noise(t *testing.T) {
	noiseLines := []string{
		`1:M 15 Jan 2024 10:23:41.456 * DB loaded from disk: 0.002 seconds`,
		`1:M 15 Jan 2024 10:23:41.789 * Background saving started by pid 42`,
		`1:C 15 Jan 2024 10:23:41.100 * DB saved on disk`,
		`1:M 15 Jan 2024 10:23:41.838 * oO0OoO0OoO0Oo Valkey is starting oO0OoO0OoO0Oo`,
		`1:M 15 Jan 2024 10:23:41.836 # WARNING Memory overcommit must be enabled!`,
	}
	for _, body := range noiseLines {
		ev, err := match(t, body)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", body, err)
		}
		if ev != nil {
			t.Errorf("expected nil for noise line, got event: EventType=%q\nbody: %s", ev.EventType, body)
		}
	}
}
