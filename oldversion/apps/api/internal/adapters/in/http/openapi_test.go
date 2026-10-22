package httpadapter

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const openAPIPath = "../../../../api/openapi.yaml"

type openAPIDoc struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Enum []string `yaml:"enum"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

var fiberParam = regexp.MustCompile(`:([A-Za-z0-9_]+)`)

// TestOpenAPIInSync falla si una ruta o un código de error existe en los
// handlers y no en api/openapi.yaml, o al revés.
func TestOpenAPIInSync(t *testing.T) {
	raw, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc openAPIDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.yaml no es YAML válido: %v", err)
	}

	var documented []string
	for path, item := range doc.Paths {
		for method := range item {
			switch method {
			case "get", "post", "put", "patch", "delete":
				documented = append(documented, strings.ToUpper(method)+" "+path)
			}
		}
	}

	app := newTestEnv(t).app
	var registered []string
	for _, r := range app.GetRoutes(true) {
		if r.Method == "HEAD" {
			continue // Fiber registra HEAD junto a cada GET.
		}
		registered = append(registered, r.Method+" "+fiberParam.ReplaceAllString(r.Path, "{$1}"))
	}

	slices.Sort(documented)
	slices.Sort(registered)
	registered = slices.Compact(registered)
	if !slices.Equal(documented, registered) {
		t.Errorf("rutas desincronizadas\nOpenAPI: %v\nFiber:   %v", documented, registered)
	}

	enum := slices.Sorted(slices.Values(doc.Components.Schemas["ErrorCode"].Enum))
	codes := slices.Sorted(slices.Values(errorCodes))
	if !slices.Equal(enum, codes) {
		t.Errorf("códigos de error desincronizados\nOpenAPI: %v\nGo:      %v", enum, codes)
	}
}
