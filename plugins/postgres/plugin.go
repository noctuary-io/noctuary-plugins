// Package postgres is the vendor plugin for PostgreSQL server logs.
// PostgreSQL log format: TIMESTAMP [PID] user@db LEVEL:  message
// Fingerprints: "LOG:  duration:", "automatic vacuum", "deadlock detected",
// "remaining connection slots".
package postgres

import (
	"regexp"
	"strconv"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

const (
	vendorName = "postgres"
	version    = "0.1.0"
)

// checkpointSlowSeconds is the minimum checkpoint total duration that is worth
// emitting as a saturation signal. Below this threshold checkpoints are routine.
// 10 seconds indicates significant I/O pressure on most production systems.
const checkpointSlowSeconds = 10.0

// tempFileLargeBytes is the minimum temp file size worth capturing. Small temp
// files (< 10MB) are routine for moderate sort/hash operations.
const tempFileLargeBytes = 10 * 1024 * 1024 // 10MB

var (
	// pgLineRe parses the standard Postgres log prefix to extract timestamp, user, db, level.
	pgLineRe = regexp.MustCompile(
		`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}[\d.]* \w+) \[(\d+)\] (?:(\w+)@(\w+) )?(LOG|ERROR|FATAL|WARNING|NOTICE):\s+(.*)`,
	)
	slowQueryRe     = regexp.MustCompile(`duration:\s+([\d.]+)\s+ms`)
	autovacRe       = regexp.MustCompile(`automatic vacuum of table "([^"]+)"`)
	autovacAnalyzeRe = regexp.MustCompile(`automatic analyze of table "([^"]+)"`)
	deadlockRe      = regexp.MustCompile(`deadlock detected`)
	connExhRe       = regexp.MustCompile(`remaining connection slots`)
	lockTimeoutRe   = regexp.MustCompile(`canceling statement due to lock timeout`)
	stmtTimeoutRe   = regexp.MustCompile(`canceling statement due to statement timeout`)
	tempFileRe      = regexp.MustCompile(`temporary file: path "([^"]+)" size (\d+)`)
	// checkpoint complete: wrote N buffers (P%); write=X s, sync=X s, total=X s
	checkpointRe = regexp.MustCompile(`checkpoint complete:.*?total=([\d.]+) s`)
	// WAL segment removed before replica could consume it — replication lag
	walRemovedRe = regexp.MustCompile(`requested WAL segment \S+ has already been removed`)
)

// Plugin implements plugin.VendorPlugin for PostgreSQL.
type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Vendor() string  { return vendorName }
func (p *Plugin) Version() string { return version }

func (p *Plugin) Fingerprints() []plugin.FingerprintRule {
	return []plugin.FingerprintRule{
		// "LOG:  duration:" with double-space is the Postgres slow query format.
		{Type: plugin.RuleTypeRegex, Value: `LOG:\s+duration:`, Weight: 0.9},
		// "automatic vacuum" and "automatic analyze" are Postgres autovacuum logging.
		{Type: plugin.RuleTypeSubstring, Value: "automatic vacuum", Weight: 0.8},
		{Type: plugin.RuleTypeSubstring, Value: "automatic analyze", Weight: 0.8},
		// "deadlock detected" is the exact Postgres error string.
		{Type: plugin.RuleTypeSubstring, Value: "deadlock detected", Weight: 0.9},
		// "remaining connection slots" is the Postgres connection exhaustion message.
		{Type: plugin.RuleTypeSubstring, Value: "remaining connection slots", Weight: 0.9},
		// "canceling statement" appears in both lock timeout and statement timeout.
		{Type: plugin.RuleTypeSubstring, Value: "canceling statement due to", Weight: 0.85},
		// "temporary file: path" is the Postgres temp file log prefix.
		{Type: plugin.RuleTypeSubstring, Value: "temporary file: path", Weight: 0.85},
		// "checkpoint complete" is a Postgres-specific operational log.
		{Type: plugin.RuleTypeSubstring, Value: "checkpoint complete:", Weight: 0.7},
		// WAL segment removal is a replication-specific Postgres message.
		{Type: plugin.RuleTypeSubstring, Value: "has already been removed", Weight: 0.8},
	}
}

func (p *Plugin) Match(line schema.ParsedOTelLog) (*schema.ContextEvent, error) {
	body := line.Body

	var ts, user, db string
	if m := pgLineRe.FindStringSubmatch(body); m != nil {
		ts, user, db = m[1], m[3], m[4]
	}
	if ts == "" {
		ts = line.Timestamp
	}
	if db == "" {
		db = line.ServiceName
	}

	switch {
	case slowQueryRe.MatchString(body):
		duration := ""
		if m := slowQueryRe.FindStringSubmatch(body); len(m) > 1 {
			duration = m[1] + "ms"
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			Actor:       user,
			NewValue:    "slow_query:" + duration,
			Timestamp:   ts,
			Confidence:  0.92,
			RawLine:     body,
			TTLSeconds:  1200,
		}, nil

	case autovacRe.MatchString(body):
		table := ""
		if m := autovacRe.FindStringSubmatch(body); len(m) > 1 {
			table = m[1]
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      table,
			NewValue:    "autovacuum",
			Timestamp:   ts,
			Confidence:  0.88,
			RawLine:     body,
			TTLSeconds:  1200,
		}, nil

	case autovacAnalyzeRe.MatchString(body):
		// Autovacuum analyze updates table statistics. Heavy analyze activity
		// means table data distribution is changing rapidly — often precedes
		// query planner making poor plan choices.
		table := ""
		if m := autovacAnalyzeRe.FindStringSubmatch(body); len(m) > 1 {
			table = m[1]
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      table,
			NewValue:    "autovacuum_analyze",
			Timestamp:   ts,
			Confidence:  0.82,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case deadlockRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			Actor:       user,
			NewValue:    "deadlock_detected",
			Timestamp:   ts,
			Confidence:  0.95,
			RawLine:     body,
			TTLSeconds:  1200,
		}, nil

	case connExhRe.MatchString(body):
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			NewValue:    "connection_limit_reached",
			Timestamp:   ts,
			Confidence:  0.97,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case lockTimeoutRe.MatchString(body):
		// A transaction waited too long to acquire a lock and was cancelled.
		// Different from deadlock: no circular dependency, just contention.
		// The cancellation means the request failed — the client will see an error.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			Actor:       user,
			NewValue:    "lock_timeout",
			Timestamp:   ts,
			Confidence:  0.93,
			RawLine:     body,
			TTLSeconds:  1200,
		}, nil

	case stmtTimeoutRe.MatchString(body):
		// A query exceeded statement_timeout and was killed. Different from a
		// slow query: the query was actively cancelled, so the request failed.
		// The client will see a query-cancelled error, not a slow response.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			Actor:       user,
			NewValue:    "statement_timeout",
			Timestamp:   ts,
			Confidence:  0.93,
			RawLine:     body,
			TTLSeconds:  1200,
		}, nil

	case tempFileRe.MatchString(body):
		// A sort or hash operation spilled to disk. Only emit for large spills
		// (> 10MB) to avoid noise from small routine operations.
		m := tempFileRe.FindStringSubmatch(body)
		if len(m) < 3 {
			return nil, nil
		}
		size, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil || size < tempFileLargeBytes {
			return nil, nil
		}
		sizeMB := size / (1024 * 1024)
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			Actor:       user,
			NewValue:    "temp_file:" + strconv.FormatInt(sizeMB, 10) + "MB",
			Timestamp:   ts,
			Confidence:  0.83,
			RawLine:     body,
			TTLSeconds:  900,
		}, nil

	case checkpointRe.MatchString(body):
		// A slow checkpoint indicates I/O saturation. Only emit if the total
		// duration exceeds the threshold — fast checkpoints are routine noise.
		m := checkpointRe.FindStringSubmatch(body)
		if len(m) < 2 {
			return nil, nil
		}
		totalSecs, err := strconv.ParseFloat(m[1], 64)
		if err != nil || totalSecs < checkpointSlowSeconds {
			return nil, nil
		}
		return &schema.ContextEvent{
			EventType:   schema.EventTypeSaturation,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			NewValue:    "slow_checkpoint:" + m[1] + "s",
			Timestamp:   ts,
			Confidence:  0.80,
			RawLine:     body,
			TTLSeconds:  1800,
		}, nil

	case walRemovedRe.MatchString(body):
		// A replica has fallen so far behind that the primary recycled WAL files
		// the replica needed. The replica must now be re-synced from scratch.
		// This is a replication dependency failure — data divergence risk.
		return &schema.ContextEvent{
			EventType:   schema.EventTypeDependencyFailure,
			Vendor:      vendorName,
			ServiceName: line.ServiceName,
			Entity:      db,
			Actor:       user,
			NewValue:    "replication_slot_invalidated",
			Timestamp:   ts,
			Confidence:  0.90,
			RawLine:     body,
			TTLSeconds:  2700,
		}, nil
	}

	return nil, nil
}
