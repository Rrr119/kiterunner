package openapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Coverage is a comparison between expected and actual route keys.
type Coverage struct {
	Expected int      `json:"expected"`
	Covered  int      `json:"covered"`
	Percent  float64  `json:"coverage_percent"`
	Missing  []string `json:"missing,omitempty"`
	Extra    []string `json:"extra,omitempty"`
}

// Complete reports whether all expected routes were generated.
func (c Coverage) Complete() bool {
	return len(c.Missing) == 0 && c.Expected == c.Covered
}

// CoverageReport summarises route generation for one spec.
type CoverageReport struct {
	Source    string    `json:"source"`
	APIID     string    `json:"api_id,omitempty"`
	Title     string    `json:"title,omitempty"`
	Version   string    `json:"version"`
	BasePath  string    `json:"base_path,omitempty"`
	ServerURL string    `json:"server_url,omitempty"`
	Spec      Coverage  `json:"spec"`
	Scannable Coverage  `json:"scannable"`
	Manual    *Coverage `json:"manual,omitempty"`
	Warnings  []string  `json:"warnings,omitempty"`

	// Convenience mirrors for dashboards that only read a top-level number.
	CoveragePercent       float64 `json:"coverage_percent"`
	ManualCoveragePercent float64 `json:"manual_coverage_percent,omitempty"`
}

var routeParamPattern = regexp.MustCompile(`\{[^{}]*\}`)

// RouteKey normalises a method and path to "METHOD /path".
func RouteKey(method, path string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return method + " " + path
}

// CanonicalRouteKey removes parameter names and query strings so two spellings
// of the same route compare as equal, e.g. /pet/{petId} and /pet/{id}.
func CanonicalRouteKey(key string) string {
	key = strings.TrimSpace(key)
	method := "GET"
	path := key
	if idx := strings.IndexAny(key, " \t"); idx >= 0 {
		method = strings.ToUpper(strings.TrimSpace(key[:idx]))
		path = strings.TrimSpace(key[idx+1:])
	}
	if len(path) >= 2 && path[0] == '/' && path[1] == '/' {
		// Leave protocol-relative values alone; they are not valid route keys
		// but we should not silently mangle them either.
	} else if idx := strings.Index(path, "://"); idx >= 0 {
		path = path[idx+3:]
		if slash := strings.Index(path, "/"); slash >= 0 {
			path = path[slash:]
		} else {
			path = "/"
		}
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if idx := strings.Index(path, "#"); idx >= 0 {
		path = path[:idx]
	}
	path = routeParamPattern.ReplaceAllString(path, "{}")
	path = strings.Replace(path, "//", "/", -1)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return method + " " + path
}

func compareCoverage(expected, actual []string) Coverage {
	expectedByCanonical := make(map[string]string, len(expected))
	for _, key := range expected {
		canonical := CanonicalRouteKey(key)
		if _, ok := expectedByCanonical[canonical]; !ok {
			expectedByCanonical[canonical] = RouteKeyFromKey(key)
		}
	}

	actualByCanonical := make(map[string]string, len(actual))
	for _, key := range actual {
		canonical := CanonicalRouteKey(key)
		if _, ok := actualByCanonical[canonical]; !ok {
			actualByCanonical[canonical] = RouteKeyFromKey(key)
		}
	}

	ret := Coverage{Expected: len(expectedByCanonical)}
	for canonical, original := range expectedByCanonical {
		if _, ok := actualByCanonical[canonical]; ok {
			ret.Covered++
		} else {
			ret.Missing = append(ret.Missing, original)
		}
	}
	for canonical, original := range actualByCanonical {
		if _, ok := expectedByCanonical[canonical]; !ok {
			ret.Extra = append(ret.Extra, original)
		}
	}

	sort.Strings(ret.Missing)
	sort.Strings(ret.Extra)
	if ret.Expected == 0 {
		ret.Percent = 100
	} else {
		ret.Percent = math.Round(float64(ret.Covered)*10000/float64(ret.Expected)) / 100
	}
	return ret
}

// CompareRoutes compares two sets of canonical METHOD /path keys.
func CompareRoutes(expected, actual []string) Coverage {
	return compareCoverage(expected, actual)
}

// RouteKeyFromKey normalises a single "METHOD /path" value while retaining its
// original path spelling.
func RouteKeyFromKey(key string) string {
	key = strings.TrimSpace(key)
	method := "GET"
	path := key
	if idx := strings.IndexAny(key, " \t"); idx >= 0 {
		method = strings.ToUpper(strings.TrimSpace(key[:idx]))
		path = strings.TrimSpace(key[idx+1:])
	}
	return RouteKey(method, path)
}

// LoadManualRoutes reads a manually curated list of routes from a file.
func LoadManualRoutes(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open manual routes: %w", err)
	}
	defer f.Close()
	return ParseManualRoutes(f)
}

// ParseManualRoutes reads routes from lines such as "GET /pets/{id}" or
// "/pets/{id}". Blank lines and lines starting with # are ignored.
func ParseManualRoutes(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	ret := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if isHTTPMethod(fields[0]) {
			path := strings.Join(fields[1:], " ")
			ret = append(ret, RouteKey(fields[0], path))
			continue
		}
		ret = append(ret, RouteKey("GET", line))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read manual routes: %w", err)
	}
	return sortUnique(ret), nil
}

func isHTTPMethod(value string) bool {
	switch strings.ToUpper(value) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE":
		return true
	default:
		return false
	}
}

// WriteText writes a human-readable coverage report.
func (r CoverageReport) WriteText(w io.Writer) error {
	title := r.Title
	if title == "" {
		title = r.Source
	}
	if _, err := fmt.Fprintf(w, "Spec: %s\n", title); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Source: %s\n", r.Source); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Version: %s\n", r.Version); err != nil {
		return err
	}
	if r.APIID != "" {
		if _, err := fmt.Fprintf(w, "API id: %s\n", r.APIID); err != nil {
			return err
		}
	}
	if r.BasePath != "" {
		if _, err := fmt.Fprintf(w, "Base path: %s\n", r.BasePath); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Spec routes: %d\n", r.Spec.Expected); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Generated routes: %d\n", r.Spec.Covered+len(r.Spec.Extra)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Route coverage: %.2f%% (%d/%d)\n", r.Spec.Percent, r.Spec.Covered, r.Spec.Expected); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Scannable coverage: %.2f%% (%d/%d)\n", r.Scannable.Percent, r.Scannable.Covered, r.Scannable.Expected); err != nil {
		return err
	}
	if r.Manual != nil {
		if _, err := fmt.Fprintf(w, "Manual baseline: %.2f%% (%d/%d)\n", r.Manual.Percent, r.Manual.Covered, r.Manual.Expected); err != nil {
			return err
		}
	}
	if err := writeRouteList(w, "Missing routes", r.Spec.Missing); err != nil {
		return err
	}
	if err := writeRouteList(w, "Unexpected routes", r.Spec.Extra); err != nil {
		return err
	}
	if r.Manual != nil {
		if err := writeRouteList(w, "Missing from manual baseline", r.Manual.Missing); err != nil {
			return err
		}
		if err := writeRouteList(w, "Unexpected vs manual baseline", r.Manual.Extra); err != nil {
			return err
		}
	}
	return writeRouteList(w, "Warnings", r.Warnings)
}

func writeRouteList(w io.Writer, heading string, values []string) error {
	if len(values) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s:\n", heading); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(w, "  - %s\n", value); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSON writes an indented JSON coverage report.
func (r CoverageReport) WriteJSON(w io.Writer) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// Write writes the coverage report in text or JSON format.
func (r CoverageReport) Write(w io.Writer, format string) error {
	switch strings.ToLower(format) {
	case "", "text", "pretty":
		return r.WriteText(w)
	case "json":
		return r.WriteJSON(w)
	default:
		return fmt.Errorf("unsupported report format: %s", format)
	}
}

// WriteReports writes one or more reports. JSON output is always an array.
func WriteReports(w io.Writer, reports []CoverageReport, format string) error {
	if !strings.EqualFold(format, "json") {
		for i, report := range reports {
			if i > 0 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			if err := report.WriteText(w); err != nil {
				return err
			}
		}
		return nil
	}

	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// WriteReportFile writes reports to a file.
func WriteReportFile(filename string, reports []CoverageReport, format string) error {
	var w io.Writer = os.Stdout
	if filename != "" {
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("failed to create report file: %w", err)
		}
		defer f.Close()
		w = f
	}
	return WriteReports(w, reports, format)
}

// ReadReportFile is a small helper used by tests and tooling.
func ReadReportFile(filename string) ([]CoverageReport, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var ret []CoverageReport
	if err := json.Unmarshal(data, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}
