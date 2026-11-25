// Package openapi provides OpenAPI/Swagger driven route generation for
// kiterunner. Documents are parsed into the same proute representation used by
// kitebuilder wordlists, so quick scans, full scans, filters and all existing
// scan options keep working unchanged.
package openapi

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"

	"github.com/assetnote/kiterunner/pkg/kitebuilder"
	"github.com/assetnote/kiterunner/pkg/proute"
)

// Document is a parsed OpenAPI/Swagger document.
type Document struct {
	Source     string
	Version    string
	Title      string
	BasePath   string
	ServerURL  string
	Operations []Operation
	Warnings   []string

	// API is the generated intermediate route document.
	API proute.API
}

// LoadFile parses an OpenAPI or Swagger document from disk.
func LoadFile(filename string) (*Document, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read openapi file: %w", err)
	}
	return LoadBytes(data, filename)
}

// ParseFile is an alias for LoadFile.
func ParseFile(filename string) (*Document, error) {
	return LoadFile(filename)
}

// LoadReader parses an OpenAPI or Swagger document from a reader.
func LoadReader(r io.Reader, source string) (*Document, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read openapi document: %w", err)
	}
	return LoadBytes(data, source)
}

// ParseReader is an alias for LoadReader.
func ParseReader(r io.Reader, source string) (*Document, error) {
	return LoadReader(r, source)
}

// LoadBytes parses JSON or YAML OpenAPI/Swagger bytes.
func LoadBytes(data []byte, source string) (*Document, error) {
	root, err := decodeDocument(data)
	if err != nil {
		return nil, err
	}

	version := asString(root["openapi"])
	if version == "" {
		version = asString(root["swagger"])
	}
	if version == "" {
		return nil, fmt.Errorf("not an OpenAPI or Swagger document: missing openapi/swagger version")
	}

	api, operations, basePath, serverURL, warnings, err := buildAPI(root, source)
	if err != nil {
		return nil, err
	}

	proutes, err := proute.FromKitebuilderAPI(*api)
	if err != nil {
		warnings = append(warnings, err.Error())
	}

	title := ""
	if info, ok := asMap(root["info"]); ok {
		title = asString(info["title"])
	}

	return &Document{
		Source:     source,
		Version:    version,
		Title:      title,
		BasePath:   basePath,
		ServerURL:  serverURL,
		Operations: operations,
		Warnings:   warnings,
		API:        proutes,
	}, nil
}

// Parse parses OpenAPI/Swagger bytes. The source is set to "<input>".
func Parse(data []byte) (*Document, error) {
	return LoadBytes(data, "<input>")
}

// LoadOpenAPIFile is an explicit alias for LoadFile.
func LoadOpenAPIFile(filename string) (*Document, error) {
	return LoadFile(filename)
}

// LoadOpenAPIBytes is an explicit alias for LoadBytes.
func LoadOpenAPIBytes(data []byte, source string) (*Document, error) {
	return LoadBytes(data, source)
}

// IsOpenAPI reports whether data decodes to an OpenAPI or Swagger document.
func IsOpenAPI(data []byte) bool {
	if !bytes.Contains(data, []byte("openapi")) && !bytes.Contains(data, []byte("swagger")) {
		return false
	}
	root, err := decodeDocument(data)
	if err != nil {
		return false
	}
	return asString(root["openapi"]) != "" || asString(root["swagger"]) != ""
}

// ToKitebuilder returns the intermediate representation for callers that want
// to compile the generated routes into a .kite wordlist.
func (d *Document) ToKitebuilder() (kitebuilder.API, error) {
	return d.API.ToKitebuilderAPI()
}

// OperationKeys returns sorted, canonical METHOD /path keys for the source
// spec operations.
func (d *Document) OperationKeys() []string {
	ret := make([]string, 0, len(d.Operations))
	for _, operation := range d.Operations {
		ret = append(ret, operation.Key())
	}
	return sortUnique(ret)
}

// GeneratedRouteKeys returns sorted, canonical METHOD /path keys for every
// route generated from the spec.
func (d *Document) GeneratedRouteKeys() []string {
	ret := make([]string, 0, len(d.API.Routes))
	for _, route := range d.API.Routes {
		ret = append(ret, RouteKey(route.Method, route.TemplatePath))
	}
	return sortUnique(ret)
}

// ScannableRouteKeys returns the generated routes that survive conversion to
// the request model used by the kiterunner engine. HEAD, OPTIONS and TRACE are
// intentionally not scanned by kiterunner today.
func (d *Document) ScannableRouteKeys() []string {
	ret := make([]string, 0, len(d.API.Routes))
	for _, route := range d.API.Routes {
		method := strings.ToUpper(route.Method)
		if !scannableMethod(method) {
			continue
		}
		if _, err := route.ToKiterunner(d.API.Headers(true)...); err != nil {
			continue
		}
		ret = append(ret, RouteKey(method, route.TemplatePath))
	}
	return sortUnique(ret)
}

// CoverageReport compares spec operations to generated routes and reports the
// routes that would actually be scanned.
func (d *Document) CoverageReport() CoverageReport {
	spec := d.OperationKeys()
	generated := d.GeneratedRouteKeys()
	scannable := d.ScannableRouteKeys()
	specCoverage := compareCoverage(spec, generated)

	return CoverageReport{
		Source:          d.Source,
		APIID:           d.API.ID,
		Title:           d.Title,
		Version:         d.Version,
		BasePath:        d.BasePath,
		ServerURL:       d.ServerURL,
		Spec:            specCoverage,
		CoveragePercent: specCoverage.Percent,
		Scannable:       compareCoverage(spec, scannable),
		Warnings:        append([]string{}, d.Warnings...),
	}
}

// Report is an alias for CoverageReport.
func (d *Document) Report() CoverageReport {
	return d.CoverageReport()
}

// CoverageReportWithManual compares generated routes against a manually
// curated list of METHOD /path entries.
func (d *Document) CoverageReportWithManual(manual []string) CoverageReport {
	ret := d.CoverageReport()
	coverage := compareCoverage(manual, d.GeneratedRouteKeys())
	ret.Manual = &coverage
	ret.ManualCoveragePercent = coverage.Percent
	return ret
}

func scannableMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func stableID(parts ...string) string {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	ret := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		ret = append(ret, value)
	}
	sort.Strings(ret)
	return ret
}
