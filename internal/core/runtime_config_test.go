package core

import (
	"encoding/json"
	"testing"
)

func TestBuildSudokuClientConfigPlacesMultiplexAtTopLevel(t *testing.T) {
	cfg := &AppConfig{
		Core:    CoreSettings{LocalPort: 1080},
		Routing: RoutingSettings{ProxyMode: "global"},
	}
	node := NodeConfig{
		ServerAddress: "example.com:8443",
		Key:           "split-private-key",
		Multiplex:     "on",
		HTTPMask: HTTPMaskSettings{
			Mode: "auto",
		},
	}

	runtimeCfg, err := buildSudokuClientConfig(cfg, node, "", false)
	if err != nil {
		t.Fatalf("build runtime config: %v", err)
	}

	raw, err := json.Marshal(runtimeCfg)
	if err != nil {
		t.Fatalf("marshal runtime config: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode runtime config: %v", err)
	}
	if got, ok := document["multiplex"].(string); !ok || got != "on" {
		t.Fatalf("top-level multiplex = %#v, want %q", document["multiplex"], "on")
	}
	httpmask, ok := document["httpmask"].(map[string]any)
	if !ok {
		t.Fatalf("httpmask has unexpected type: %#v", document["httpmask"])
	}
	if _, exists := httpmask["multiplex"]; exists {
		t.Fatalf("httpmask unexpectedly contains multiplex: %#v", httpmask)
	}
}
