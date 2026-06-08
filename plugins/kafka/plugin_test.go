package kafka_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/kafka"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return kafka.New().Match(schema.ParsedOTelLog{
		ServiceName: "kafka",
		Body:        body,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if kafka.New().Vendor() != "kafka" {
		t.Error("Vendor() should be kafka")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	if len(kafka.New().Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func TestPlugin_Match_RebalanceTrigger(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantGroup string
	}{
		{
			name:      "rebalance triggered with group ID",
			body:      `[2024-01-15 10:23:41,123] INFO Preparing to rebalance group my-consumer-group in generation 5 (kafka.coordinator.group.GroupCoordinator)`,
			wantGroup: "my-consumer-group",
		},
		{
			name:      "rebalance triggered with hyphenated group",
			body:      `[2024-01-15 10:23:41,456] INFO Preparing to rebalance group otel-demo-checkout-group in generation 1 (kafka.coordinator.group.GroupCoordinator)`,
			wantGroup: "otel-demo-checkout-group",
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
			if ev.Entity != tt.wantGroup {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantGroup)
			}
			if ev.NewValue != "rebalance_triggered" {
				t.Errorf("NewValue = %q, want rebalance_triggered", ev.NewValue)
			}
		})
	}
}

func TestPlugin_Match_RebalanceComplete(t *testing.T) {
	body := `[2024-01-15 10:23:42,100] INFO Stabilized group my-consumer-group generation 6 (kafka.coordinator.group.GroupCoordinator)`
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
	if ev.NewValue != "rebalance_complete" {
		t.Errorf("NewValue = %q, want rebalance_complete", ev.NewValue)
	}
}

func TestPlugin_Match_UnderReplicatedPartitions(t *testing.T) {
	body := `[2024-01-15 10:23:43,200] ERROR [KafkaController id=1] Under-replicated partitions: my-topic-0, my-topic-1 (kafka.controller.KafkaController)`
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
	if ev.NewValue != "under_replicated_partitions" {
		t.Errorf("NewValue = %q, want under_replicated_partitions", ev.NewValue)
	}
}

func TestPlugin_Match_BrokerShutdown(t *testing.T) {
	body := `[2024-01-15 10:23:44,300] INFO Broker 2 has requested controlled shutdown (kafka.server.KafkaServer)`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeRestart {
		t.Errorf("EventType = %q, want restart", ev.EventType)
	}
	if ev.Entity != "broker_2" {
		t.Errorf("Entity = %q, want broker_2", ev.Entity)
	}
}

func TestPlugin_Match_Noise(t *testing.T) {
	noiseLines := []string{
		`[2024-01-15 10:23:41,123] INFO Starting (kafka.server.KafkaServer)`,
		`[2024-01-15 10:23:41,456] DEBUG Fetching metadata (kafka.network.RequestChannel)`,
		`[2024-01-15 10:23:41,789] INFO [Partition my-topic-0, brokerId=0] Completed load of log (kafka.cluster.Partition)`,
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
