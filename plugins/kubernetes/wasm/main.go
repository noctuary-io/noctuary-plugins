//go:build wasip1

package main

import (
	"encoding/json"
	"os"

	"github.com/noctuary-io/noctuary-plugins/plugin"
	"github.com/noctuary-io/noctuary-plugins/plugins/kubernetes"
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
	p := kubernetes.New()

	var req request
	json.NewDecoder(os.Stdin).Decode(&req)

	enc := json.NewEncoder(os.Stdout)
	switch req.Method {
	case "fingerprints":
		enc.Encode(response{Fingerprints: p.Fingerprints()})
	case "match":
		if req.Line != nil {
			event, _ := p.Match(*req.Line)
			enc.Encode(response{Event: event})
		}
	}
}
