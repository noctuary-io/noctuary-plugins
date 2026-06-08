package generic_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/generic"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body, severity string, attrs map[string]string) (*schema.ContextEvent, error) {
	t.Helper()
	return generic.New().Match(schema.ParsedOTelLog{
		ServiceName: "test-service",
		Body:        body,
		Severity:    severity,
		Attributes:  attrs,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if generic.New().Vendor() != "generic" {
		t.Error("Vendor() should be generic")
	}
}

func TestPlugin_FingerprintMatchesEveryLine(t *testing.T) {
	p := generic.New()
	fps := p.Fingerprints()
	if len(fps) == 0 {
		t.Fatal("Fingerprints() must not be empty")
	}
	// The catch-all rule must score exactly 0.5 (the default threshold).
	total := 0.0
	for _, fp := range fps {
		total += fp.Weight
	}
	if total != 0.5 {
		t.Errorf("total fingerprint weight = %.2f, want exactly 0.5 (threshold)", total)
	}
}

func TestPlugin_Match_ErrorSeverity(t *testing.T) {
	tests := []struct {
		name       string
		severity   string
		body       string
		wantEvent  bool
		wantType   schema.EventType
	}{
		{
			name:      "ERROR severity fires",
			severity:  "ERROR",
			body:      "connection refused to downstream service",
			wantEvent: true,
			wantType:  schema.EventTypeDependencyFailure,
		},
		{
			name:      "error lowercase fires",
			severity:  "error",
			body:      "some error message",
			wantEvent: true,
			wantType:  schema.EventTypeDependencyFailure,
		},
		{
			name:      "ERROR2 sub-level fires",
			severity:  "ERROR2",
			body:      "error at sub-level 2",
			wantEvent: true,
			wantType:  schema.EventTypeDependencyFailure,
		},
		{
			name:      "FATAL severity fires as restart",
			severity:  "FATAL",
			body:      "fatal: unrecoverable state",
			wantEvent: true,
			wantType:  schema.EventTypeRestart,
		},
		{
			name:      "FATAL2 fires as restart",
			severity:  "FATAL2",
			body:      "fatal sub-level",
			wantEvent: true,
			wantType:  schema.EventTypeRestart,
		},
		{
			name:      "INFO severity is ignored",
			severity:  "INFO",
			body:      "service started successfully",
			wantEvent: false,
		},
		{
			name:      "WARN severity is ignored",
			severity:  "WARN",
			body:      "slow response detected",
			wantEvent: false,
		},
		{
			name:      "empty severity is ignored",
			severity:  "",
			body:      "some message",
			wantEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body, tt.severity, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantEvent && ev == nil {
				t.Fatal("expected event, got nil")
			}
			if !tt.wantEvent && ev != nil {
				t.Fatalf("expected nil, got event: %+v", ev)
			}
			if ev != nil && ev.EventType != tt.wantType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.wantType)
			}
		})
	}
}

func TestPlugin_Match_ExceptionAttributes(t *testing.T) {
	attrs := map[string]string{
		"exception.type":    "java.net.ConnectException",
		"exception.message": "Connection refused",
	}
	ev, err := match(t, "some info body", "INFO", attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event when exception.type is set, even at INFO severity")
	}
	if ev.NewValue != "java.net.ConnectException: Connection refused" {
		t.Errorf("NewValue = %q, want exception type: message", ev.NewValue)
	}
}

func TestPlugin_Match_BodyTruncation(t *testing.T) {
	longBody := "this is a very long error message that exceeds the eighty character limit and should be truncated"
	ev, err := match(t, longBody, "ERROR", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if len(ev.NewValue) > 80 {
		t.Errorf("NewValue length = %d, want <= 80", len(ev.NewValue))
	}
}

func TestPlugin_Match_LowConfidence(t *testing.T) {
	ev, err := match(t, "error message", "ERROR", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Confidence >= 0.5 {
		t.Errorf("Confidence = %.2f, want < 0.5 (generic should score below specific plugins)", ev.Confidence)
	}
}

func TestPlugin_Match_RequiredFields(t *testing.T) {
	ev, err := match(t, "error occurred", "ERROR", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Vendor != "generic" {
		t.Errorf("Vendor = %q, want generic", ev.Vendor)
	}
	if ev.Entity == "" {
		t.Error("Entity must not be empty")
	}
	if ev.TTLSeconds <= 0 {
		t.Errorf("TTLSeconds = %d, want > 0", ev.TTLSeconds)
	}
	if ev.RawLine == "" {
		t.Error("RawLine must not be empty")
	}
}
