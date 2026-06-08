//go:build wasip1

// WASM entry point for the myplugin vendor plugin.
// Build with: GOOS=wasip1 GOARCH=wasm go build -o myplugin.wasm ./wasm/
package main

import (
	"encoding/json"
	"os"

	"github.com/noctuary-io/noctuary-plugins/_template/myplugin"
	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

type request struct {
	Method string               `json:"method"`
	Line   *schema.ParsedOTelLog `json:"line,omitempty"`
}

type response struct {
	Fingerprints []plugin.FingerprintRule `json:"fingerprints,omitempty"`
	Event        *schema.ContextEvent     `json:"event,omitempty"`
}

func main() {
	p := myplugin.New()

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
