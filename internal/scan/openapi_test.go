package scan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadOpenAPIFileGeneratesScanRoutes(t *testing.T) {
	options := NewDefaultScanOptions()
	filename := "../../pkg/openapi/testdata/petstore-v3.yaml"

	assert.NoError(t, LoadOpenAPIFile([]string{filename})(options))
	assert.Len(t, options.prouteAPIs, 1)
	assert.Len(t, options.Routes, 11)
	assert.Len(t, options.openapiReports, 1)
	assert.Equal(t, filename, options.openapiNames[0])
	assert.Equal(t, 100.0, options.openapiReports[0].CoveragePercent)

	// Quick scan source filtering depends on every route carrying the API ID.
	apiID := options.prouteAPIs[0].ID
	for _, route := range options.Routes {
		assert.Equal(t, apiID, route.Source)
	}
}

func TestLoadKitebuilderFileAutoDetectsOpenAPI(t *testing.T) {
	for _, filename := range []string{
		"../../pkg/openapi/testdata/petstore-v2.json",
		"../../pkg/openapi/testdata/bookstore-v3.json",
	} {
		options := NewDefaultScanOptions()
		assert.NoError(t, LoadKitebuilderFile([]string{filename})(options), filename)
		assert.NotEmpty(t, options.prouteAPIs, filename)
		assert.NotEmpty(t, options.Routes, filename)
		assert.Len(t, options.openapiReports, 1, filename)
	}
}

func TestOpenAPICoverageReportOption(t *testing.T) {
	options := NewDefaultScanOptions()
	assert.NoError(t, OpenAPICoverageReport(true, "")(options))
	assert.True(t, options.printOpenAPIReports)
}
