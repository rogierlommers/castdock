package api

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocumentIsValidYAMLAndCoversJSONEndpoints(t *testing.T) {
	contents, err := Files.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document struct {
		OpenAPI string                 `yaml:"openapi"`
		Paths   map[string]interface{} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("OpenAPI YAML error = %v", err)
	}
	if document.OpenAPI != "3.0.3" {
		t.Fatalf("openapi = %q", document.OpenAPI)
	}
	for _, path := range []string{"/healthz", "/login", "/logout", "/api/episodes/{id}/playback", "/episodes/{id}/played", "/api/queue/reorder", "/feeds/{id}/playback-options", "/library/import", "/library/export.opml"} {
		if _, ok := document.Paths[path]; !ok {
			t.Errorf("OpenAPI document missing %s", path)
		}
	}
}
