package plugin

import "fmt"

// Registry holds the set of active vendor plugins.
type Registry struct {
	plugins map[string]VendorPlugin
}

func NewRegistry() *Registry {
	return &Registry{plugins: make(map[string]VendorPlugin)}
}

func (r *Registry) Register(p VendorPlugin) error {
	if _, exists := r.plugins[p.Vendor()]; exists {
		return fmt.Errorf("plugin already registered: %s", p.Vendor())
	}
	r.plugins[p.Vendor()] = p
	return nil
}

func (r *Registry) All() []VendorPlugin {
	out := make([]VendorPlugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p)
	}
	return out
}

func (r *Registry) Get(vendor string) (VendorPlugin, bool) {
	p, ok := r.plugins[vendor]
	return p, ok
}
