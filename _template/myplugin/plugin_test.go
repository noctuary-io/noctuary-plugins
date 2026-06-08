package myplugin_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/_template/myplugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func TestFingerprints(t *testing.T) {
	p := myplugin.New()
	fps := p.Fingerprints()
	if len(fps) == 0 {
		t.Fatal("expected at least one fingerprint rule")
	}
	for _, r := range fps {
		if r.Weight <= 0 || r.Weight > 1 {
			t.Errorf("rule %q weight %v out of range (0,1]", r.Value, r.Weight)
		}
	}
}

func TestMatch_Restart(t *testing.T) {
	p := myplugin.New()
	line := schema.ParsedOTelLog{
		ServiceName: "my-service",
		Body:        "my vendor started",
		Timestamp:   "2024-01-01T00:00:00Z",
	}
	ev, err := p.Match(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected an event, got nil")
	}
	if ev.EventType != schema.EventTypeRestart {
		t.Errorf("expected restart event, got %q", ev.EventType)
	}
}

func TestMatch_Unrecognised(t *testing.T) {
	p := myplugin.New()
	line := schema.ParsedOTelLog{
		ServiceName: "my-service",
		Body:        "some unrelated log line",
		Timestamp:   "2024-01-01T00:00:00Z",
	}
	ev, err := p.Match(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil event for unrecognised line, got %+v", ev)
	}
}
