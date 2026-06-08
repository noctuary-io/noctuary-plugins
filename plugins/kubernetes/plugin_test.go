package kubernetes_test

import (
	"encoding/json"
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/kubernetes"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

// k8sBody returns a JSON-encoded Kubernetes Event object as a log body string.
func k8sBody(reason, message, objKind, objNamespace, objName, evType string, count int) string {
	raw := map[string]any{
		"kind":          "Event",
		"apiVersion":    "v1",
		"reason":        reason,
		"message":       message,
		"type":          evType,
		"count":         count,
		"lastTimestamp": "2024-01-15T10:23:41Z",
		"involvedObject": map[string]any{
			"kind":      objKind,
			"namespace": objNamespace,
			"name":      objName,
		},
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

func line(body string) schema.ParsedOTelLog {
	return schema.ParsedOTelLog{ServiceName: "test-service", Body: body}
}

func mustMatch(t *testing.T, body string) *schema.ContextEvent {
	t.Helper()
	ev, err := kubernetes.New().Match(line(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	return ev
}

func mustNil(t *testing.T, body string) {
	t.Helper()
	ev, err := kubernetes.New().Match(line(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil, got event: reason implied=%q EventType=%q NewValue=%q", ev.NewValue, ev.EventType, ev.NewValue)
	}
}

// ---- OOM events -------------------------------------------------------------

func TestMatch_OOMKilling(t *testing.T) {
	t.Run("OOMKilling extracts memory limit", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("OOMKilling", "Memory limit reached. MemoryLimit: 512Mi",
			"Pod", "production", "checkout-service-7d6f8b-xyz", "Warning", 3))
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart", ev.EventType)
		}
		if ev.Entity != "checkout-service-7d6f8b-xyz" {
			t.Errorf("Entity = %q", ev.Entity)
		}
		if ev.NewValue != "OOMKilled" {
			t.Errorf("NewValue = %q, want OOMKilled", ev.NewValue)
		}
		if ev.OldValue != "limit=512Mi" {
			t.Errorf("OldValue = %q, want limit=512Mi (memory limit is critical for diagnosis)", ev.OldValue)
		}
		if ev.TTLSeconds != 1200 {
			t.Errorf("TTLSeconds = %d, want 1200", ev.TTLSeconds)
		}
		if ev.Confidence < 0.90 {
			t.Errorf("Confidence = %.2f, want >= 0.90", ev.Confidence)
		}
	})

	t.Run("OOMKilling with 1Gi limit", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("OOMKilling", "Memory cgroup out of memory. MemoryLimit: 1Gi",
			"Pod", "production", "api-server-abc", "Warning", 1))
		if ev.OldValue != "limit=1Gi" {
			t.Errorf("OldValue = %q, want limit=1Gi", ev.OldValue)
		}
	})

	t.Run("OOMKilling without memory limit in message — OldValue empty but event still captured", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("OOMKilling", "Container svc was OOM killed",
			"Pod", "production", "svc-abc", "Warning", 1))
		if ev.NewValue != "OOMKilled" {
			t.Errorf("NewValue = %q, want OOMKilled", ev.NewValue)
		}
		// OldValue may be empty when message doesn't include the limit
	})

	t.Run("OOMKilled past-tense reason code (kubelet sometimes uses this variant)", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("OOMKilled", "Container checkout was OOM killed",
			"Pod", "production", "checkout-service-abc", "Warning", 1))
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart", ev.EventType)
		}
		if ev.NewValue != "OOMKilled" {
			t.Errorf("NewValue = %q, want OOMKilled", ev.NewValue)
		}
	})

	t.Run("OOMKilling repeated event (count > 1) — same classification", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("OOMKilling", "Memory limit reached. MemoryLimit: 256Mi",
			"Pod", "production", "svc-pod", "Warning", 12))
		if ev.NewValue != "OOMKilled" {
			t.Errorf("NewValue = %q", ev.NewValue)
		}
	})
}

// ---- CrashLoop events -------------------------------------------------------

func TestMatch_CrashLoop(t *testing.T) {
	t.Run("BackOff crash loop restarting", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("BackOff",
			"Back-off restarting failed container payment in pod payment-service-6c9f-abc_production(xyz)",
			"Pod", "production", "payment-service-6c9f-abc", "Warning", 15))
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart", ev.EventType)
		}
		if ev.NewValue != "CrashLoopBackOff" {
			t.Errorf("NewValue = %q, want CrashLoopBackOff", ev.NewValue)
		}
		if ev.TTLSeconds != 1200 {
			t.Errorf("TTLSeconds = %d, want 1200", ev.TTLSeconds)
		}
		if ev.Confidence < 0.85 {
			t.Errorf("Confidence = %.2f, want >= 0.85", ev.Confidence)
		}
	})

	t.Run("BackOff crash loop with exit code 1 — app crash", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("BackOff",
			"Back-off restarting failed container svc in pod svc-abc_production(xyz) (exit code 1)",
			"Pod", "production", "svc-abc", "Warning", 5))
		if ev.OldValue != "exit_code=1" {
			t.Errorf("OldValue = %q, want exit_code=1 (exit code 1 = app crash, different from OOM kill exit 137)", ev.OldValue)
		}
	})

	t.Run("BackOff crash loop with exit code 137 — OOM kill by container runtime", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("BackOff",
			"Back-off restarting failed container svc in pod svc-abc (exit code 137)",
			"Pod", "production", "svc-abc", "Warning", 8))
		if ev.OldValue != "exit_code=137" {
			t.Errorf("OldValue = %q, want exit_code=137 (137 = SIGKILL from container OOM)", ev.OldValue)
		}
	})

	t.Run("BackOff crash loop with exit code 2 — misconfiguration", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("BackOff",
			"Back-off restarting failed container svc in pod svc-abc (exit code 2)",
			"Pod", "production", "svc-abc", "Warning", 3))
		if ev.OldValue != "exit_code=2" {
			t.Errorf("OldValue = %q, want exit_code=2", ev.OldValue)
		}
	})

	t.Run("BackOff image pull — must return nil, not a crash loop", func(t *testing.T) {
		mustNil(t, k8sBody("BackOff", "Back-off pulling image myapp:latest",
			"Pod", "production", "some-pod-abc", "Warning", 5))
	})

	t.Run("BackOff pulling image (lowercase) — also nil", func(t *testing.T) {
		mustNil(t, k8sBody("BackOff", "back-off pulling image ghcr.io/myorg/myapp:v1.2.3",
			"Pod", "production", "svc-pod", "Warning", 3))
	})

	t.Run("BackOff ImagePullBackOff message — nil", func(t *testing.T) {
		mustNil(t, k8sBody("BackOff", "Back-off pulling image \"myrepo/myimage:latest\"",
			"Pod", "production", "svc-pod", "Warning", 2))
	})
}

// ---- Scale events -----------------------------------------------------------

func TestMatch_ScaleEvents(t *testing.T) {
	t.Run("ScalingReplicaSet scale-up from Deployment controller", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("ScalingReplicaSet",
			"Scaled up replica set checkout-service-7d6f8b from 3 to 5",
			"ReplicaSet", "production", "checkout-service-7d6f8b", "Normal", 1))
		if ev.EventType != schema.EventTypeScaleEvent {
			t.Errorf("EventType = %q, want scale_event", ev.EventType)
		}
		if ev.OldValue != "3" || ev.NewValue != "5" {
			t.Errorf("OldValue=%q NewValue=%q, want 3→5", ev.OldValue, ev.NewValue)
		}
		if ev.TTLSeconds != 1800 {
			t.Errorf("TTLSeconds = %d, want 1800", ev.TTLSeconds)
		}
	})

	t.Run("ScalingReplicaSet scale-down — load reduced or rolling update", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("ScalingReplicaSet",
			"Scaled down replica set checkout-service-7d6f8b from 5 to 3",
			"ReplicaSet", "production", "checkout-service-7d6f8b", "Normal", 1))
		if ev.OldValue != "5" || ev.NewValue != "3" {
			t.Errorf("OldValue=%q NewValue=%q, want 5→3 (scale-down is the resolution signal)", ev.OldValue, ev.NewValue)
		}
	})

	t.Run("ScalingReplicaSet scale from zero — cold start", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("ScalingReplicaSet",
			"Scaled up replica set checkout-service-new from 0 to 3",
			"ReplicaSet", "production", "checkout-service-new", "Normal", 1))
		if ev.OldValue != "0" || ev.NewValue != "3" {
			t.Errorf("OldValue=%q NewValue=%q, want 0→3", ev.OldValue, ev.NewValue)
		}
	})

	t.Run("SuccessfulRescale from HPA — CPU-based autoscaler decision", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("SuccessfulRescale",
			"New size: 5; reason: cpu resource utilization (percentage of request) above target",
			"HorizontalPodAutoscaler", "production", "checkout-hpa", "Normal", 1))
		if ev.EventType != schema.EventTypeScaleEvent {
			t.Errorf("EventType = %q, want scale_event", ev.EventType)
		}
		if ev.NewValue != "5" {
			t.Errorf("NewValue = %q, want 5 (new replica count from HPA decision)", ev.NewValue)
		}
	})

	t.Run("SuccessfulRescale from HPA — memory-based autoscaler decision", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("SuccessfulRescale",
			"New size: 8; reason: memory resource utilization (percentage of request) above target",
			"HorizontalPodAutoscaler", "production", "payment-hpa", "Normal", 1))
		if ev.NewValue != "8" {
			t.Errorf("NewValue = %q, want 8", ev.NewValue)
		}
	})

	t.Run("SuccessfulRescale scale-down from HPA — load normalised", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("SuccessfulRescale",
			"New size: 3; reason: All metrics below target",
			"HorizontalPodAutoscaler", "production", "checkout-hpa", "Normal", 1))
		if ev.NewValue != "3" {
			t.Errorf("NewValue = %q, want 3", ev.NewValue)
		}
	})
}

// ---- Node pressure events ---------------------------------------------------

func TestMatch_NodePressure(t *testing.T) {
	t.Run("NodeNotReady — kubelet has not reported in", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("NodeNotReady",
			"Node worker-node-1 status is now: NodeNotReady",
			"Node", "", "worker-node-1", "Warning", 1))
		if ev.EventType != schema.EventTypeDependencyFailure {
			t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
		}
		if ev.NewValue != "NodeNotReady" {
			t.Errorf("NewValue = %q, want NodeNotReady", ev.NewValue)
		}
		if ev.TTLSeconds != 2700 {
			t.Errorf("TTLSeconds = %d, want 2700 (node failure has longest causal window)", ev.TTLSeconds)
		}
		if ev.Confidence < 0.80 {
			t.Errorf("Confidence = %.2f, want >= 0.80", ev.Confidence)
		}
	})

	t.Run("NodeMemoryPressure — node running low on memory, evictions imminent", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("NodeMemoryPressure",
			"Node worker-node-2 status is now: NodeMemoryPressure",
			"Node", "", "worker-node-2", "Warning", 1))
		if ev.EventType != schema.EventTypeDependencyFailure {
			t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
		}
		if ev.NewValue != "NodeMemoryPressure" {
			t.Errorf("NewValue = %q, want NodeMemoryPressure", ev.NewValue)
		}
		if ev.TTLSeconds != 2700 {
			t.Errorf("TTLSeconds = %d, want 2700", ev.TTLSeconds)
		}
	})

	t.Run("NodeDiskPressure — node disk nearly full, evictions imminent", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("NodeDiskPressure",
			"Node worker-node-3 status is now: NodeDiskPressure",
			"Node", "", "worker-node-3", "Warning", 1))
		if ev.EventType != schema.EventTypeDependencyFailure {
			t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
		}
		if ev.NewValue != "NodeDiskPressure" {
			t.Errorf("NewValue = %q, want NodeDiskPressure", ev.NewValue)
		}
	})

	t.Run("EvictionThresholdMet — kubelet has begun evicting pods", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("EvictionThresholdMet",
			"Attempting to reclaim memory",
			"Node", "", "worker-node-1", "Warning", 1))
		if ev.EventType != schema.EventTypeDependencyFailure {
			t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
		}
		if ev.NewValue != "EvictionThresholdMet" {
			t.Errorf("NewValue = %q, want EvictionThresholdMet", ev.NewValue)
		}
	})

	t.Run("Node Warning event with unknown reason — caught by Node+Warning catch-all", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("KernelDeadlock",
			"Node has kernel deadlock",
			"Node", "", "worker-node-1", "Warning", 1))
		if ev.EventType != schema.EventTypeDependencyFailure {
			t.Errorf("EventType = %q, want dependency_failure (node Warning events are always dependency failures)", ev.EventType)
		}
	})
}

// ---- Eviction events --------------------------------------------------------

func TestMatch_Eviction(t *testing.T) {
	t.Run("Evicted due to memory pressure", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Evicted",
			"The node was low on resource: memory. Threshold quantity: 100Mi, available: 15Mi.",
			"Pod", "production", "checkout-service-xyz", "Warning", 1))
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart", ev.EventType)
		}
		if ev.NewValue != "Evicted" {
			t.Errorf("NewValue = %q, want Evicted", ev.NewValue)
		}
		if ev.OldValue != "memory_pressure" {
			t.Errorf("OldValue = %q, want memory_pressure (eviction reason distinguishes memory from disk)", ev.OldValue)
		}
		if ev.TTLSeconds != 1200 {
			t.Errorf("TTLSeconds = %d, want 1200", ev.TTLSeconds)
		}
	})

	t.Run("Evicted due to disk pressure — ephemeral storage limit", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Evicted",
			"The node was low on resource: ephemeral-storage. Threshold quantity: 10Gi, available: 500Mi.",
			"Pod", "production", "payment-service-abc", "Warning", 1))
		if ev.OldValue != "disk_pressure" {
			t.Errorf("OldValue = %q, want disk_pressure", ev.OldValue)
		}
	})

	t.Run("Evicted due to pid pressure", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Evicted",
			"The node was low on resource: pids. Threshold quantity: 100, available: 5.",
			"Pod", "production", "svc-pod", "Warning", 1))
		if ev.OldValue != "pid_pressure" {
			t.Errorf("OldValue = %q, want pid_pressure", ev.OldValue)
		}
	})

	t.Run("Evicted with no specific reason in message — OldValue empty", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Evicted", "Pod was evicted.",
			"Pod", "production", "svc-pod", "Warning", 1))
		if ev.NewValue != "Evicted" {
			t.Errorf("NewValue = %q, want Evicted", ev.NewValue)
		}
	})
}

// ---- Probe failure events ---------------------------------------------------

func TestMatch_ProbeFailures(t *testing.T) {
	t.Run("Liveness probe failure — container will be restarted", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Unhealthy",
			"Liveness probe failed: HTTP probe failed with statuscode: 503",
			"Pod", "production", "checkout-service-xyz", "Warning", 3))
		if ev.EventType != schema.EventTypeRestart {
			t.Errorf("EventType = %q, want restart (liveness failure causes container restart)", ev.EventType)
		}
		if ev.NewValue != "probe_failure" {
			t.Errorf("NewValue = %q, want probe_failure", ev.NewValue)
		}
		if ev.OldValue != "liveness" {
			t.Errorf("OldValue = %q, want liveness (LLM needs probe type to distinguish restart vs traffic removal)", ev.OldValue)
		}
		if ev.TTLSeconds != 900 {
			t.Errorf("TTLSeconds = %d, want 900 (probe failures are transient signals)", ev.TTLSeconds)
		}
	})

	t.Run("Readiness probe failure — pod removed from Service endpoints, no restart", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Unhealthy",
			"Readiness probe failed: Get \"http://10.0.0.1:8080/healthz\": context deadline exceeded",
			"Pod", "production", "payment-service-abc", "Warning", 5))
		if ev.OldValue != "readiness" {
			t.Errorf("OldValue = %q, want readiness", ev.OldValue)
		}
	})

	t.Run("Startup probe failure — container taking too long to initialise", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Unhealthy",
			"Startup probe failed: exec command exited with exit code 1",
			"Pod", "production", "slow-startup-abc", "Warning", 2))
		if ev.OldValue != "startup" {
			t.Errorf("OldValue = %q, want startup", ev.OldValue)
		}
	})

	t.Run("Unhealthy with unrecognised probe type — OldValue empty, still captured", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("Unhealthy",
			"Container failed health check",
			"Pod", "production", "svc-abc", "Warning", 1))
		if ev.NewValue != "probe_failure" {
			t.Errorf("NewValue = %q, want probe_failure", ev.NewValue)
		}
	})
}

// ---- FailedScheduling events -----------------------------------------------

func TestMatch_FailedScheduling(t *testing.T) {
	t.Run("FailedScheduling insufficient memory — cluster capacity crunch", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("FailedScheduling",
			"0/3 nodes are available: 3 Insufficient memory. preemption: 0/3 nodes are available: 3 No preemption victims found for incoming pod.",
			"Pod", "production", "checkout-service-newpod", "Warning", 1))
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation (scheduling failure = cluster can't absorb load)", ev.EventType)
		}
		if ev.NewValue != "FailedScheduling" {
			t.Errorf("NewValue = %q, want FailedScheduling", ev.NewValue)
		}
		if ev.OldValue != "insufficient_memory" {
			t.Errorf("OldValue = %q, want insufficient_memory (constraint type directs engineer to the right fix)", ev.OldValue)
		}
		if ev.TTLSeconds != 1800 {
			t.Errorf("TTLSeconds = %d, want 1800 (scheduling failures persist until cluster capacity changes)", ev.TTLSeconds)
		}
	})

	t.Run("FailedScheduling insufficient CPU", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("FailedScheduling",
			"0/5 nodes are available: 5 Insufficient cpu.",
			"Pod", "production", "api-gateway-newpod", "Warning", 1))
		if ev.OldValue != "insufficient_cpu" {
			t.Errorf("OldValue = %q, want insufficient_cpu", ev.OldValue)
		}
	})

	t.Run("FailedScheduling taints/tolerations — no matching node", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("FailedScheduling",
			"0/3 nodes are available: 3 node(s) had untolerated taint {dedicated: gpu}.",
			"Pod", "production", "ml-job-pod", "Warning", 1))
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		// OldValue empty for taint mismatch — LLM still knows scheduling failed
	})
}

// ---- Noise: events that must return nil ------------------------------------

func TestMatch_Noise(t *testing.T) {
	noiseEvents := []struct {
		name   string
		reason string
		msg    string
		kind   string
		ns     string
		name_  string
		evType string
	}{
		{
			name:   "Pulled image — container lifecycle noise",
			reason: "Pulled",
			msg:    "Successfully pulled image \"myrepo/myapp:v1.2.3\" in 3.456s",
			kind:   "Pod", ns: "production", name_: "svc-pod", evType: "Normal",
		},
		{
			name:   "Scheduled — pod assigned to node, operational noise",
			reason: "Scheduled",
			msg:    "Successfully assigned production/svc-pod to worker-node-1",
			kind:   "Pod", ns: "production", name_: "svc-pod", evType: "Normal",
		},
		{
			name:   "Created container — lifecycle noise",
			reason: "Created",
			msg:    "Created container svc",
			kind:   "Pod", ns: "production", name_: "svc-pod", evType: "Normal",
		},
		{
			name:   "Started container — lifecycle noise",
			reason: "Started",
			msg:    "Started container svc",
			kind:   "Pod", ns: "production", name_: "svc-pod", evType: "Normal",
		},
		{
			name:   "Killing container — graceful termination during rolling update",
			reason: "Killing",
			msg:    "Stopping container svc",
			kind:   "Pod", ns: "production", name_: "svc-pod", evType: "Normal",
		},
		{
			name:   "SuccessfulCreate — replicaset created a pod",
			reason: "SuccessfulCreate",
			msg:    "Created pod: svc-pod-abc",
			kind:   "ReplicaSet", ns: "production", name_: "svc-replicaset", evType: "Normal",
		},
		{
			name:   "Pulling image — normal pre-start activity",
			reason: "Pulling",
			msg:    "Pulling image \"myrepo/myapp:v1.2.3\"",
			kind:   "Pod", ns: "production", name_: "svc-pod", evType: "Normal",
		},
	}

	for _, tt := range noiseEvents {
		t.Run(tt.name, func(t *testing.T) {
			mustNil(t, k8sBody(tt.reason, tt.msg, tt.kind, tt.ns, tt.name_, tt.evType, 1))
		})
	}
}

// ---- Edge cases: structural/parse failures ---------------------------------

func TestMatch_EdgeCases(t *testing.T) {
	t.Run("Non-JSON log line — no match", func(t *testing.T) {
		mustNil(t, `time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=checkout dest-server=https://k8s.local`)
	})

	t.Run("JSON but not a k8s event — no match", func(t *testing.T) {
		mustNil(t, `{"level":"info","msg":"server started","port":8080}`)
	})

	t.Run("JSON with empty reason — no match", func(t *testing.T) {
		raw := map[string]any{
			"kind": "Event", "reason": "", "message": "something",
			"type": "Warning", "count": 1, "lastTimestamp": "2024-01-15T10:23:41Z",
			"involvedObject": map[string]any{"kind": "Pod", "namespace": "production", "name": "svc-pod"},
		}
		b, _ := json.Marshal(raw)
		mustNil(t, string(b))
	})

	t.Run("JSON with empty involvedObject name — no match", func(t *testing.T) {
		raw := map[string]any{
			"kind": "Event", "reason": "OOMKilling", "message": "Memory limit reached. MemoryLimit: 512Mi",
			"type": "Warning", "count": 1, "lastTimestamp": "2024-01-15T10:23:41Z",
			"involvedObject": map[string]any{"kind": "Pod", "namespace": "production", "name": ""},
		}
		b, _ := json.Marshal(raw)
		mustNil(t, string(b))
	})

	t.Run("Valid OOMKilling event has all required schema fields", func(t *testing.T) {
		ev := mustMatch(t, k8sBody("OOMKilling", "Memory limit reached. MemoryLimit: 512Mi",
			"Pod", "production", "svc-pod", "Warning", 1))
		if ev.Vendor != "kubernetes" {
			t.Errorf("Vendor = %q, want kubernetes", ev.Vendor)
		}
		if ev.Timestamp == "" {
			t.Error("Timestamp empty")
		}
		if ev.RawLine == "" {
			t.Error("RawLine empty")
		}
		if ev.Confidence <= 0 || ev.Confidence > 1 {
			t.Errorf("Confidence = %.2f out of (0, 1]", ev.Confidence)
		}
		if ev.TTLSeconds <= 0 {
			t.Errorf("TTLSeconds = %d, must be > 0", ev.TTLSeconds)
		}
	})
}

// ---- Cross-contamination guard ---------------------------------------------

func TestDoesNotFireOnArgoCD(t *testing.T) {
	argoLines := []string{
		`time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=checkout dest-server=https://kubernetes.default.svc`,
		`time="2024-01-15T10:23:55Z" level=info msg="Sync operation succeeded" app=svc revision=abc1234`,
		`time="2024-01-15T10:24:30Z" level=warning msg="App health changed" app=svc from=Healthy to=Degraded`,
	}
	for _, body := range argoLines {
		mustNil(t, body)
	}
}

func TestDoesNotFireOnPostgres(t *testing.T) {
	pgLines := []string{
		`2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT 1`,
		`2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`,
		`2024-01-15 10:23:42.000 UTC [1237] FATAL:  remaining connection slots are reserved`,
	}
	for _, body := range pgLines {
		mustNil(t, body)
	}
}
