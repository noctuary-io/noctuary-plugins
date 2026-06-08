//go:build wasip1

// WASM entry point for the ArgoCD vendor plugin.
// Compiled with: GOOS=wasip1 GOARCH=wasm go build -o argocd.wasm ./plugins/argocd/wasm/
//
// # Communication protocol
//
// Command mode (not reactor): a fresh module instance is started per call.
// The host writes one JSON Request to the module's stdin; the module writes
// one JSON Response to stdout and exits. The compiled module is cached by the
// host loader so only instantiation (not recompilation) occurs per call.
//
// Request:  {"method":"fingerprints"} | {"method":"match","line":{...ParsedOTelLog}}
// Response: {"fingerprints":[...]} | {"event":{...ContextEvent}} | {"event":null}
package main

import (
	"encoding/json"
	"os"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/plugins/argocd"
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
	p := argocd.New()

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
