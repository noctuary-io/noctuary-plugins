GO           := go
GOFLAGS_WASM := GOOS=wasip1 GOARCH=wasm

PLUGINS := argocd flagd generic gojson kafka kubernetes log4j2 pino postgres redis

.PHONY: all wasm test clean $(PLUGINS)

## all: build WASM binaries for all plugins
all: wasm

## wasm: build WASM for every plugin
wasm: $(foreach p,$(PLUGINS),plugins/$(p)/$(p).wasm)

## <name>: build WASM for a single plugin, e.g.  make redis
$(PLUGINS): %: plugins/%/%.wasm

plugins/%/%.wasm: plugins/%/wasm/main.go plugins/%/plugin.go
	$(GOFLAGS_WASM) $(GO) build -o $@ ./plugins/$*/wasm/

## test: run all plugin tests
test:
	$(GO) test ./...

## clean: remove compiled WASM binaries
clean:
	find plugins -name "*.wasm" -delete
