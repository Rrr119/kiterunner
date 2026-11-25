package openapi

import (
	"github.com/assetnote/kiterunner/pkg/kitebuilder"
)

// schemaFromValue converts a Swagger/OpenAPI schema into the kitebuilder
// schema used by proute. Reference cycles are cut off with an empty object
// schema so recursive models cannot make the route generator recurse forever.
func schemaFromValue(root interface{}, raw interface{}, stack map[string]bool) *kitebuilder.Schema {
	m, ok := asMap(raw)
	if !ok || len(m) == 0 {
		return &kitebuilder.Schema{Type: "object"}
	}

	if ref, ok := m["$ref"].(string); ok && ref != "" {
		if stack[ref] {
			return &kitebuilder.Schema{Type: "object"}
		}

		target, err := lookupJSONPointer(root, ref)
		if err != nil {
			return &kitebuilder.Schema{Type: "object"}
		}

		stack[ref] = true
		ret := schemaFromValue(root, target, stack)
		delete(stack, ref)

		if len(m) > 1 {
			ret = mergeSchemas(ret, schemaFromValue(root, withoutRef(m), stack))
		}
		return ret
	}

	ret := &kitebuilder.Schema{}
	ret.Type = schemaType(m)
	ret.Title = asString(m["title"])
	ret.Format = asString(m["format"])
	ret.Description = asString(m["description"])
	ret.Pattern = asString(m["pattern"])
	ret.Name = asString(m["name"])
	ret.CollectionFormat = asString(m["collectionFormat"])
	ret.Nullable = asBool(m["nullable"])
	ret.ReadOnly = asBool(m["readOnly"])
	ret.WriteOnly = asBool(m["writeOnly"])
	ret.AllowEmptyValue = asBool(m["allowEmptyValue"])
	ret.Enum = asInterfaceSlice(m["enum"])
	ret.Min = firstFloat(m["minimum"], m["min"])
	ret.Max = firstFloat(m["maximum"], m["max"])
	ret.MultipleOf = firstFloat(m["multipleOf"])
	ret.MinLength = asUint(m["minLength"])
	ret.MaxLength = asUint(m["maxLength"])
	ret.MinItems = asUint(m["minItems"])
	ret.MaxItems = asUint(m["maxItems"])
	ret.MinProps = asUint(m["minProperties"])
	ret.MaxProps = asUint(m["maxProperties"])
	ret.ExclusiveMin = asBool(m["exclusiveMinimum"])
	ret.ExclusiveMax = asBool(m["exclusiveMaximum"])
	ret.UniqueItems = asBool(m["uniqueItems"])

	if required, ok := m["required"]; ok {
		switch required.(type) {
		case bool:
			ret.Required = required
		default:
			if names := asStringSlice(required); len(names) > 0 {
				ret.Required = names
			}
		}
	}

	if properties, ok := asMap(m["properties"]); ok {
		ret.Properties = make(map[string]kitebuilder.Schema, len(properties))
		for name, value := range properties {
			ret.Properties[name] = *schemaFromValue(root, value, stack)
		}
	}

	if items, ok := m["items"]; ok {
		ret.Items = schemaFromValue(root, items, stack)
	}

	if additional, ok := m["additionalProperties"]; ok {
		if additionalMap, isMap := asMap(additional); isMap {
			ret.AdditionalProperties = schemaFromValue(root, additionalMap, stack)
		} else if asBool(additional) {
			ret.AdditionalProperties = &kitebuilder.Schema{Type: "object"}
		}
	}

	if allOf, ok := asSlice(m["allOf"]); ok {
		for _, item := range allOf {
			ret = mergeSchemas(ret, schemaFromValue(root, item, stack))
		}
	}

	// For route/body generation a deterministic representative schema is more
	// useful than preserving every union branch.
	for _, key := range []string{"oneOf", "anyOf"} {
		if options, ok := asSlice(m[key]); ok && len(options) > 0 {
			ret = mergeSchemas(ret, schemaFromValue(root, options[0], stack))
			break
		}
	}

	value := m["default"]
	if value == nil {
		value = m["example"]
	}
	if value == nil && len(ret.Enum) > 0 {
		value = ret.Enum[0]
	}
	if value != nil {
		ret.Default = value
	}
	if example, ok := m["example"]; ok {
		ret.Example = example
	}

	return ret
}

func schemaType(m map[string]interface{}) string {
	if t, ok := m["type"].(string); ok && t != "" {
		return t
	}
	if types, ok := asSlice(m["type"]); ok {
		for _, item := range types {
			if t, ok := item.(string); ok && t != "" && t != "null" {
				return t
			}
		}
	}
	if _, ok := m["properties"]; ok {
		return "object"
	}
	if _, ok := m["additionalProperties"]; ok {
		return "object"
	}
	if _, ok := m["items"]; ok {
		return "array"
	}
	if _, ok := m["allOf"]; ok {
		return "object"
	}
	return ""
}

func mergeSchemas(base, overlay *kitebuilder.Schema) *kitebuilder.Schema {
	if base == nil {
		return overlay
	}
	if overlay == nil {
		return base
	}

	ret := *base
	if overlay.Type != "" {
		ret.Type = overlay.Type
	}
	if overlay.Title != "" {
		ret.Title = overlay.Title
	}
	if overlay.Format != "" {
		ret.Format = overlay.Format
	}
	if overlay.Description != "" {
		ret.Description = overlay.Description
	}
	if overlay.Pattern != "" {
		ret.Pattern = overlay.Pattern
	}
	if overlay.Name != "" {
		ret.Name = overlay.Name
	}
	if overlay.CollectionFormat != "" {
		ret.CollectionFormat = overlay.CollectionFormat
	}
	if overlay.Default != nil {
		ret.Default = overlay.Default
	}
	if overlay.Example != nil {
		ret.Example = overlay.Example
	}
	if len(overlay.Enum) > 0 {
		ret.Enum = overlay.Enum
	}
	if overlay.Required != nil {
		ret.Required = overlay.Required
	}
	if overlay.Items != nil {
		ret.Items = overlay.Items
	}
	if overlay.AdditionalProperties != nil {
		ret.AdditionalProperties = overlay.AdditionalProperties
	}
	if len(overlay.Properties) > 0 {
		if ret.Properties == nil {
			ret.Properties = make(map[string]kitebuilder.Schema, len(overlay.Properties))
		}
		for k, v := range overlay.Properties {
			ret.Properties[k] = v
		}
	}
	ret.AllOf = append(ret.AllOf, overlay.AllOf...)
	return &ret
}

func asInterfaceSlice(v interface{}) []interface{} {
	if values, ok := asSlice(v); ok {
		return values
	}
	return nil
}

func firstFloat(values ...interface{}) float64 {
	for _, value := range values {
		if f, ok := asFloat(value); ok {
			return f
		}
	}
	return 0
}
