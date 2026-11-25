package openapi

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

// decodeDocument decodes either JSON or YAML. The returned value is normalised
// to map[string]interface{} throughout so the parser does not need to care
// about the input format.
func decodeDocument(data []byte) (map[string]interface{}, error) {
	var raw interface{}

	if err := json.Unmarshal(data, &raw); err != nil {
		if yamlErr := yaml.Unmarshal(data, &raw); yamlErr != nil {
			return nil, fmt.Errorf("failed to decode document as json or yaml: json: %v, yaml: %v", err, yamlErr)
		}
	}

	raw = normalizeValue(raw)
	root, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("document root must be an object, got %T", raw)
	}
	return root, nil
}

// normalizeValue converts the map[interface{}]interface{} values produced by
// yaml.v2 into JSON-compatible values.
func normalizeValue(v interface{}) interface{} {
	switch vv := v.(type) {
	case map[interface{}]interface{}:
		ret := make(map[string]interface{}, len(vv))
		for k, val := range vv {
			ret[fmt.Sprintf("%v", k)] = normalizeValue(val)
		}
		return ret
	case map[string]interface{}:
		ret := make(map[string]interface{}, len(vv))
		for k, val := range vv {
			ret[k] = normalizeValue(val)
		}
		return ret
	case []interface{}:
		ret := make([]interface{}, len(vv))
		for i, val := range vv {
			ret[i] = normalizeValue(val)
		}
		return ret
	default:
		return v
	}
}

func asMap(v interface{}) (map[string]interface{}, bool) {
	ret, ok := v.(map[string]interface{})
	return ret, ok
}

func asSlice(v interface{}) ([]interface{}, bool) {
	ret, ok := v.([]interface{})
	return ret, ok
}

func asString(v interface{}) string {
	switch vv := v.(type) {
	case string:
		return vv
	case fmt.Stringer:
		return vv.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", vv)
	}
}

func asBool(v interface{}) bool {
	switch vv := v.(type) {
	case bool:
		return vv
	case string:
		b, _ := strconv.ParseBool(vv)
		return b
	default:
		return false
	}
}

func asFloat(v interface{}) (float64, bool) {
	switch vv := v.(type) {
	case float64:
		return vv, true
	case float32:
		return float64(vv), true
	case int:
		return float64(vv), true
	case int32:
		return float64(vv), true
	case int64:
		return float64(vv), true
	case json.Number:
		f, err := vv.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(vv, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func asUint(v interface{}) uint64 {
	f, ok := asFloat(v)
	if !ok || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return uint64(f)
}

func asStringSlice(v interface{}) []string {
	switch vv := v.(type) {
	case []string:
		return append([]string{}, vv...)
	case []interface{}:
		ret := make([]string, 0, len(vv))
		for _, item := range vv {
			ret = append(ret, asString(item))
		}
		return ret
	default:
		return nil
	}
}

func sortedKeys(m map[string]interface{}) []string {
	ret := make([]string, 0, len(m))
	for k := range m {
		ret = append(ret, k)
	}
	sort.Strings(ret)
	return ret
}

// lookupJSONPointer resolves a local OpenAPI $ref such as
// #/components/schemas/Pet. External references are intentionally not
// followed; returning an error lets callers use a safe fallback instead.
func lookupJSONPointer(root interface{}, ref string) (interface{}, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("unsupported external reference: %s", ref)
	}

	cur := root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.Replace(strings.Replace(part, "~1", "/", -1), "~0", "~", -1)

		switch vv := cur.(type) {
		case map[string]interface{}:
			next, ok := vv[part]
			if !ok {
				return nil, fmt.Errorf("reference %s: key %q not found", ref, part)
			}
			cur = next
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(vv) {
				return nil, fmt.Errorf("reference %s: invalid array index %q", ref, part)
			}
			cur = vv[idx]
		default:
			return nil, fmt.Errorf("reference %s: cannot descend into %T", ref, cur)
		}
	}
	return cur, nil
}

// resolveReference follows local $ref objects. Sibling fields are shallow
// merged over the target, matching the JSON Reference behaviour used by many
// OpenAPI producers.
func resolveReference(root interface{}, raw interface{}, stack map[string]bool) (interface{}, error) {
	m, ok := asMap(raw)
	if !ok {
		return raw, nil
	}

	ref, hasRef := m["$ref"]
	refString, isString := ref.(string)
	if !hasRef || !isString || refString == "" {
		return raw, nil
	}

	if stack[refString] {
		return nil, fmt.Errorf("cyclic reference: %s", refString)
	}

	target, err := lookupJSONPointer(root, refString)
	if err != nil {
		return nil, err
	}

	stack[refString] = true
	resolved, err := resolveReference(root, target, stack)
	delete(stack, refString)
	if err != nil {
		return nil, err
	}

	resolvedMap, ok := asMap(resolved)
	if !ok {
		return resolved, nil
	}

	ret := make(map[string]interface{}, len(resolvedMap)+len(m))
	for k, v := range resolvedMap {
		ret[k] = v
	}
	for k, v := range m {
		if k == "$ref" {
			continue
		}
		ret[k] = v
	}
	return ret, nil
}

func withoutRef(m map[string]interface{}) map[string]interface{} {
	ret := make(map[string]interface{}, len(m))
	for k, v := range m {
		if k == "$ref" {
			continue
		}
		ret[k] = v
	}
	return ret
}
