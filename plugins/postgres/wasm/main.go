//go:build wasip1

// WASM entry point for the PostgreSQL vendor plugin.
// See plugins/argocd/wasm/main.go for the full protocol description.
package main

import (
	"encoding/json"
	"os"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/plugins/postgres"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

type request struct {
	Method string              `json:"method"`
	Line   *schema.ParsedOTelLog `json:"line,omitempty"`
}

type response struct {
	Fingerprints []plugin.FingerprintRule `json:"fingerprints,omitempty"`
	Event        *schema.ContextEvent     `json:"event,omitempty"`
}

func main() {
	p := postgres.New()

	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)

	switch req.Method {
	case "fingerprints":
		enc.Encode(response{Fingerprints: p.Fingerprints()})

	case "match":
		if req.Line == nil {
			enc.Encode(response{})
			return
		}
		event, err := p.Match(*req.Line)
		if err != nil {
			os.Exit(1)
		}
		enc.Encode(response{Event: event})
	}
}
