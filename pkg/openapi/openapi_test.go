package openapi

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/assetnote/kiterunner/pkg/kitebuilder"
	"github.com/assetnote/kiterunner/pkg/proute"
	"github.com/stretchr/testify/assert"
)

func TestThreeSpecsMatchManualBaseline(t *testing.T) {
	tests := []struct {
		name       string
		spec       string
		manual     string
		operations int
	}{
		{
			name:       "swagger-2-petstore",
			spec:       "testdata/petstore-v2.json",
			manual:     "testdata/petstore-v2.routes.txt",
			operations: 6,
		},
		{
			name:       "openapi-3-petstore",
			spec:       "testdata/petstore-v3.yaml",
			manual:     "testdata/petstore-v3.routes.txt",
			operations: 11,
		},
		{
			name:       "openapi-3-bookstore",
			spec:       "testdata/bookstore-v3.json",
			manual:     "testdata/bookstore-v3.routes.txt",
			operations: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := LoadFile(tt.spec)
			assert.NoError(t, err)
			if doc == nil {
				return
			}

			manual, err := LoadManualRoutes(tt.manual)
			assert.NoError(t, err)
			report := doc.CoverageReportWithManual(manual)

			assert.Equal(t, tt.operations, len(doc.Operations))
			assert.Equal(t, tt.operations, len(doc.API.Routes))
			assert.Equal(t, 100.0, report.Spec.Percent, "spec -> generated coverage")
			assert.True(t, report.Spec.Complete(), "missing generated routes: %v", report.Spec.Missing)
			assert.Equal(t, 100.0, report.Scannable.Percent, "spec -> scannable coverage")
			assert.Equal(t, 100.0, report.Manual.Percent, "generated -> manual coverage")
			assert.True(t, report.Manual.Complete(), "manual diff: missing=%v extra=%v", report.Manual.Missing, report.Manual.Extra)
			assert.Empty(t, report.Warnings)

			t.Logf("%s: %d/%d routes match manual baseline", tt.name, report.Manual.Covered, report.Manual.Expected)
		})
	}
}

func TestSwagger2RoutesAndParameters(t *testing.T) {
	doc, err := LoadFile("testdata/petstore-v2.json")
	assert.NoError(t, err)
	assert.Equal(t, "2.0", doc.Version)
	assert.Equal(t, "/v2", doc.BasePath)
	assert.Equal(t, "https://petstore.swagger.io/v2", doc.ServerURL)
	assert.Equal(t, "Petstore (Swagger 2)", doc.Title)

	getPet := findRoute(t, doc.API, "get", "/v2/pet/{petId}")
	assert.Len(t, getPet.PathCrumbs, 1)
	assert.Equal(t, "petId", getPet.PathCrumbs[0].Key())
	assert.Len(t, getPet.HeaderCrumbs, 1)
	assert.Equal(t, "X-Request-Id", getPet.HeaderCrumbs[0].Key())

	rendered, err := getPet.Path(true)
	assert.NoError(t, err)
	assert.Contains(t, rendered, "/v2/pet/")
	assert.NotContains(t, rendered, "{")

	postPet := findRoute(t, doc.API, "post", "/v2/pet")
	assert.Len(t, postPet.BodyCrumbs, 1)
	assert.Contains(t, string(postPet.Body(true, proute.ContentTypeJSON)), "doggie")
	assert.Contains(t, postPet.ContentType, proute.ContentTypeJSON)

	// The security definition is applied to the API so it is included in every
	// generated request.
	assert.Len(t, doc.API.HeaderCrumbs, 1)
	assert.Equal(t, "api_key", doc.API.HeaderCrumbs[0].Key())
}

func TestOpenAPI3RoutesAndComponents(t *testing.T) {
	doc, err := LoadFile("testdata/petstore-v3.yaml")
	assert.NoError(t, err)
	assert.Equal(t, "3.0.3", doc.Version)
	assert.Equal(t, "/api/v3", doc.BasePath)

	postPet := findRoute(t, doc.API, "post", "/api/v3/pet")
	assert.Len(t, postPet.BodyCrumbs, 1)
	assert.Contains(t, postPet.ContentType, proute.ContentTypeJSON)
	assert.Contains(t, postPet.ContentType, proute.ContentType("application/xml"))
	assert.Contains(t, string(postPet.Body(true, proute.ContentTypeJSON)), "doggie")

	findByStatus := findRoute(t, doc.API, "get", "/api/v3/pet/findByStatus")
	assert.Len(t, findByStatus.QueryCrumbs, 1)
	assert.Equal(t, "status", findByStatus.QueryCrumbs[0].Key())

	getPet := findRoute(t, doc.API, "get", "/api/v3/pet/{petId}")
	assert.Len(t, getPet.PathCrumbs, 1)
	assert.Equal(t, "petId", getPet.PathCrumbs[0].Key())
}

func TestServerVariablesAndRecursiveSchemas(t *testing.T) {
	doc, err := LoadFile("testdata/bookstore-v3.json")
	assert.NoError(t, err)
	assert.Equal(t, "/api/v1", doc.BasePath)

	createBook := findRoute(t, doc.API, "post", "/api/v1/books")
	assert.Len(t, createBook.BodyCrumbs, 1)
	body := string(createBook.Body(true, proute.ContentTypeJSON))
	assert.Contains(t, body, "The Left Hand of Darkness")

	// A recursive Author -> Book -> Author model must terminate.
	assert.NotEmpty(t, body)
	assert.Len(t, doc.API.HeaderCrumbs, 1)
	assert.Equal(t, "Authorization", doc.API.HeaderCrumbs[0].Key())
}

func TestCoverageDetectsMissingAndExtraRoutes(t *testing.T) {
	doc, err := LoadFile("testdata/petstore-v2.json")
	assert.NoError(t, err)

	manual := []string{
		"GET /v2/pet/{id}", // different parameter name, same canonical route
		"GET /v2/not-in-spec",
	}
	report := doc.CoverageReportWithManual(manual)

	assert.Equal(t, 2, report.Manual.Expected)
	assert.Equal(t, 1, report.Manual.Covered)
	assert.Equal(t, 50.0, report.Manual.Percent)
	assert.Contains(t, report.Manual.Missing, "GET /v2/not-in-spec")
	assert.NotEmpty(t, report.Manual.Extra)
	assert.Contains(t, report.Manual.Extra, "POST /v2/pet")
}

func TestParseManualRoutes(t *testing.T) {
	input := `
# comment
GET /pets/{petId}
post   /pets
/health
DELETE /pets/{id} # inline comment
`
	routes, err := ParseManualRoutes(strings.NewReader(input))
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"DELETE /pets/{id}",
		"GET /health",
		"GET /pets/{petId}",
		"POST /pets",
	}, routes)

	assert.Equal(t, "GET /pets/{}", CanonicalRouteKey("get /pets/{petId}"))
	assert.Equal(t, "GET /pets/{}", CanonicalRouteKey("GET /pets/{id}?verbose=true"))
}

func TestIsOpenAPI(t *testing.T) {
	for _, name := range []string{
		"testdata/petstore-v2.json",
		"testdata/petstore-v3.yaml",
		"testdata/bookstore-v3.json",
	} {
		data, err := ioutil.ReadFile(name)
		assert.NoError(t, err)
		assert.True(t, IsOpenAPI(data), name)
	}
	assert.False(t, IsOpenAPI([]byte("not a spec")))
	assert.False(t, IsOpenAPI([]byte(`{"ksuid":"x","paths":{}}`)))
}

func TestOpenAPI31TypeUnion(t *testing.T) {
	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": {"title": "union", "version": "1"},
	  "paths": {
	    "/items/{id}": {
	      "get": {
	        "operationId": "getItem",
	        "parameters": [{
	          "name": "id",
	          "in": "path",
	          "required": true,
	          "schema": {"type": ["integer", "null"], "default": 5}
	        }],
	        "responses": {"200": {"description": "ok"}}
	      }
	    }
	  }
	}`)

	doc, err := LoadBytes(spec, "union.json")
	assert.NoError(t, err)
	assert.Equal(t, "3.1.0", doc.Version)
	assert.Equal(t, 100.0, doc.CoverageReport().Spec.Percent)
	route := findRoute(t, doc.API, "get", "/items/{id}")
	assert.Len(t, route.PathCrumbs, 1)
	assert.Equal(t, "5", route.PathCrumbs[0].Value())
}

func TestToKitebuilderRoundTrip(t *testing.T) {
	doc, err := LoadFile("testdata/petstore-v3.yaml")
	assert.NoError(t, err)

	api, err := doc.ToKitebuilder()
	assert.NoError(t, err)
	assert.Equal(t, doc.API.ID, api.ID)
	assert.NotEmpty(t, api.Paths)

	converted, err := proute.FromKitebuilderAPI(api)
	assert.NoError(t, err)
	assert.Len(t, converted.Routes, len(doc.API.Routes))
}

func TestCoverageReportJSONFile(t *testing.T) {
	doc, err := LoadFile("testdata/bookstore-v3.json")
	assert.NoError(t, err)
	manual, err := LoadManualRoutes("testdata/bookstore-v3.routes.txt")
	assert.NoError(t, err)

	report := doc.CoverageReportWithManual(manual)
	f, err := ioutil.TempFile("", "kiterunner-openapi-*.json")
	assert.NoError(t, err)
	name := f.Name()
	defer os.Remove(name)
	assert.NoError(t, f.Close())

	assert.NoError(t, WriteReportFile(name, []CoverageReport{report}, "json"))
	reports, err := ReadReportFile(name)
	assert.NoError(t, err)
	assert.Len(t, reports, 1)
	assert.Equal(t, 100.0, reports[0].Manual.Percent)
}

func findRoute(t *testing.T, api proute.API, method, path string) proute.Route {
	t.Helper()
	for _, route := range api.Routes {
		if strings.EqualFold(route.Method, method) && route.TemplatePath == path {
			return route
		}
	}
	t.Fatalf("route %s %s not found", strings.ToUpper(method), path)
	return proute.Route{}
}

func TestDocumentToKitebuilderSchemaCompatibility(t *testing.T) {
	doc, err := LoadFile("testdata/petstore-v2.json")
	assert.NoError(t, err)
	api, err := doc.ToKitebuilder()
	assert.NoError(t, err)

	var operation kitebuilder.Operation
	for _, operations := range api.Paths {
		if op, ok := operations[kitebuilder.POST]; ok {
			operation = op
			break
		}
	}
	assert.NotEmpty(t, operation.Parameters)
}
