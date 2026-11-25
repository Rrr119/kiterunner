package openapi

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/assetnote/kiterunner/pkg/kitebuilder"
)

// Operation is an OpenAPI path + method pair. It is kept separately from the
// generated proute so coverage can be calculated even if a generator warning
// caused a route to be skipped.
type Operation struct {
	Method      string
	Path        string
	OperationID string
}

// Key returns the canonical METHOD /path key for this operation.
func (o Operation) Key() string {
	return RouteKey(o.Method, o.Path)
}

func buildAPI(root map[string]interface{}, source string) (*kitebuilder.API, []Operation, string, string, []string, error) {
	warnings := make([]string, 0)
	version := asString(root["openapi"])
	if version == "" {
		version = asString(root["swagger"])
	}

	title := ""
	if info, ok := asMap(root["info"]); ok {
		title = asString(info["title"])
	}

	basePath, serverURL := documentBasePath(root, version)

	api := &kitebuilder.API{
		ID:                  stableID(source, version, title, basePath),
		URL:                 serverURL,
		SecurityDefinitions: securityDefinitions(root, version),
		Paths:               make(map[kitebuilder.Path]kitebuilder.Operations),
	}
	if api.URL == "" {
		api.URL = title
	}
	if api.URL == "" {
		api.URL = source
	}

	paths, ok := asMap(root["paths"])
	if !ok {
		return nil, nil, "", "", warnings, fmt.Errorf("openapi document has no paths object")
	}

	globalConsumes := asStringSlice(root["consumes"])
	operations := make([]Operation, 0)

	for _, specPath := range sortedKeys(paths) {
		rawPathItem := paths[specPath]
		pathItem, err := resolveReference(root, rawPathItem, make(map[string]bool))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", specPath, err))
			pathItem = rawPathItem
		}
		pathMap, ok := asMap(pathItem)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s: path item is not an object", specPath))
			continue
		}

		fullPath := joinPaths(basePath, specPath)
		pathParameters := parseParameterList(root, pathMap["parameters"], &warnings)
		if _, ok := api.Paths[kitebuilder.Path(fullPath)]; !ok {
			api.Paths[kitebuilder.Path(fullPath)] = make(kitebuilder.Operations)
		}

		for _, method := range httpMethods {
			rawOperation, ok := pathMap[method]
			if !ok {
				continue
			}

			rawOperation, err := resolveReference(root, rawOperation, make(map[string]bool))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s %s: %v", strings.ToUpper(method), fullPath, err))
				continue
			}
			operationMap, ok := asMap(rawOperation)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("%s %s: operation is not an object", strings.ToUpper(method), fullPath))
				continue
			}

			parameters := mergeParameters(pathParameters, parseParameterList(root, operationMap["parameters"], &warnings))
			consumes := operationConsumes(root, operationMap, globalConsumes, version)

			if requestBody, contentType, ok := parseRequestBody(root, operationMap, &warnings); ok {
				parameters = append(parameters, requestBody)
				if len(contentType) > 0 {
					consumes = contentType
				}
			}

			operationID := asString(operationMap["operationId"])
			operations = append(operations, Operation{
				Method:      strings.ToUpper(method),
				Path:        fullPath,
				OperationID: operationID,
			})

			api.Paths[kitebuilder.Path(fullPath)][kitebuilder.OperationTypes(method)] = kitebuilder.Operation{
				Description: asString(operationMap["description"]),
				OperationID: operationID,
				Parameters:  parameters,
				Consumes:    toContentTypes(consumes),
				Produces:    toContentTypes(operationProduces(root, operationMap)),
			}
		}
	}

	sort.Slice(operations, func(i, j int) bool {
		return operations[i].Key() < operations[j].Key()
	})

	return api, operations, basePath, serverURL, warnings, nil
}

var httpMethods = []string{
	"get", "post", "put", "patch", "delete", "head", "options", "trace",
}

func parseParameterList(root interface{}, raw interface{}, warnings *[]string) []kitebuilder.Parameter {
	values, ok := asSlice(raw)
	if !ok {
		return nil
	}

	ret := make([]kitebuilder.Parameter, 0, len(values))
	for _, value := range values {
		resolved, err := resolveReference(root, value, make(map[string]bool))
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("parameter: %v", err))
			continue
		}
		param, err := parameterFromValue(root, resolved)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("parameter: %v", err))
			continue
		}
		ret = append(ret, param)
	}
	return ret
}

func mergeParameters(base, operation []kitebuilder.Parameter) []kitebuilder.Parameter {
	if len(base) == 0 {
		return operation
	}
	if len(operation) == 0 {
		return base
	}

	overridden := make(map[string]bool, len(operation))
	for _, param := range operation {
		overridden[param.Name+"\x00"+strings.ToLower(param.In)] = true
	}

	ret := make([]kitebuilder.Parameter, 0, len(base)+len(operation))
	for _, param := range base {
		if overridden[param.Name+"\x00"+strings.ToLower(param.In)] {
			continue
		}
		ret = append(ret, param)
	}
	return append(ret, operation...)
}

func parameterFromValue(root interface{}, raw interface{}) (kitebuilder.Parameter, error) {
	m, ok := asMap(raw)
	if !ok {
		return kitebuilder.Parameter{}, fmt.Errorf("parameter is not an object")
	}

	paramType := ""
	if value, ok := m["type"].(string); ok {
		paramType = value
	}
	ret := kitebuilder.Parameter{
		Description:     asString(m["description"]),
		In:              asString(m["in"]),
		Name:            asString(m["name"]),
		Required:        m["required"],
		Type:            paramType,
		Format:          asString(m["format"]),
		Pattern:         asString(m["pattern"]),
		Example:         m["example"],
		Default:         m["default"],
		Minimum:         firstFloat(m["minimum"]),
		Maximum:         firstFloat(m["maximum"]),
		MaxLength:       asUint(m["maxLength"]),
		MaxItems:        asUint(m["maxItems"]),
		MinLength:       asUint(m["minLength"]),
		MinItems:        asUint(m["minItems"]),
		Enum:            asInterfaceSlice(m["enum"]),
		AllowEmptyValue: asBool(m["allowEmptyValue"]),
	}

	if schemaValue, ok := m["schema"]; ok && schemaValue != nil {
		ret.Schema = schemaFromValue(root, schemaValue, make(map[string]bool))
		// Leave Type empty so proute reads the schema's default/type instead of
		// treating the parameter itself as a primitive.
		ret.Type = ""
	} else if content, ok := asMap(m["content"]); ok {
		_, media, _ := firstMediaType(content)
		if media != nil {
			if schemaValue, ok := media["schema"]; ok {
				ret.Schema = schemaFromValue(root, schemaValue, make(map[string]bool))
				ret.Type = ""
			}
		}
	}

	if ret.Default == nil && ret.Example != nil {
		ret.Default = ret.Example
	}
	if ret.Default == nil && len(ret.Enum) > 0 {
		ret.Default = ret.Enum[0]
	}

	if items, ok := m["items"]; ok {
		ret.Items = schemaFromValue(root, items, make(map[string]bool))
	}

	return ret, nil
}

func parseRequestBody(root interface{}, operation map[string]interface{}, warnings *[]string) (kitebuilder.Parameter, []string, bool) {
	raw, ok := operation["requestBody"]
	if !ok {
		return kitebuilder.Parameter{}, nil, false
	}

	resolved, err := resolveReference(root, raw, make(map[string]bool))
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("requestBody: %v", err))
		return kitebuilder.Parameter{}, nil, false
	}
	body, ok := asMap(resolved)
	if !ok {
		*warnings = append(*warnings, "requestBody is not an object")
		return kitebuilder.Parameter{}, nil, false
	}

	content, ok := asMap(body["content"])
	if !ok {
		return kitebuilder.Parameter{
			Name:     "body",
			In:       "body",
			Required: body["required"],
			Schema:   &kitebuilder.Schema{Type: "object"},
		}, nil, true
	}

	contentTypes := sortedKeys(content)
	_, media, _ := firstMediaType(content)
	if media == nil {
		media = make(map[string]interface{})
	}

	schema := &kitebuilder.Schema{Type: "object"}
	if schemaValue, ok := media["schema"]; ok {
		schema = schemaFromValue(root, schemaValue, make(map[string]bool))
	}

	return kitebuilder.Parameter{
		Name:        "body",
		In:          "body",
		Required:    body["required"],
		Description: asString(body["description"]),
		Schema:      schema,
	}, contentTypes, true
}

func firstMediaType(content map[string]interface{}) (string, map[string]interface{}, error) {
	if _, ok := content["application/json"]; ok {
		media, _ := asMap(content["application/json"])
		return "application/json", media, nil
	}
	for _, name := range sortedKeys(content) {
		media, ok := asMap(content[name])
		if !ok {
			continue
		}
		return name, media, nil
	}
	return "", nil, nil
}

func operationConsumes(root map[string]interface{}, operation map[string]interface{}, global []string, version string) []string {
	if version != "" && !strings.HasPrefix(version, "2") {
		if _, ok := operation["requestBody"]; ok {
			return nil
		}
		return nil
	}

	consumes := asStringSlice(operation["consumes"])
	if len(consumes) > 0 {
		return consumes
	}
	if len(global) > 0 {
		return global
	}
	return nil
}

func operationProduces(root map[string]interface{}, operation map[string]interface{}) []string {
	produces := asStringSlice(operation["produces"])
	if len(produces) > 0 {
		return produces
	}
	return asStringSlice(root["produces"])
}

func toContentTypes(values []string) []kitebuilder.ContentType {
	ret := make([]kitebuilder.ContentType, 0, len(values))
	for _, value := range values {
		if value != "" {
			ret = append(ret, kitebuilder.ContentType(value))
		}
	}
	return ret
}

func securityDefinitions(root map[string]interface{}, version string) map[string]kitebuilder.SecurityDefinition {
	ret := make(map[string]kitebuilder.SecurityDefinition)

	raw := map[string]interface{}{}
	if strings.HasPrefix(version, "3") {
		if components, ok := asMap(root["components"]); ok {
			if schemes, ok := asMap(components["securitySchemes"]); ok {
				raw = schemes
			}
		}
	} else if schemes, ok := asMap(root["securityDefinitions"]); ok {
		raw = schemes
	}

	for name, value := range raw {
		resolved, err := resolveReference(root, value, make(map[string]bool))
		if err != nil {
			continue
		}
		scheme, ok := asMap(resolved)
		if !ok {
			continue
		}

		def := kitebuilder.SecurityDefinition{Name: name}
		switch strings.ToLower(asString(scheme["type"])) {
		case "apikey":
			def.Type = "apikey"
			def.In = strings.ToLower(asString(scheme["in"]))
			if name := asString(scheme["name"]); name != "" {
				def.Name = name
			}
		case "basic":
			def.Type = "basic"
			def.In = "header"
			def.Name = "Authorization"
		case "http":
			def.In = "header"
			def.Name = "Authorization"
			if strings.EqualFold(asString(scheme["scheme"]), "basic") {
				def.Type = "basic"
			} else {
				def.Type = "apikey"
			}
		case "oauth2", "openidconnect":
			def.Type = "apikey"
			def.In = "header"
			def.Name = "Authorization"
		default:
			continue
		}
		ret[name] = def
	}
	return ret
}

func documentBasePath(root map[string]interface{}, version string) (string, string) {
	if strings.HasPrefix(version, "3") {
		servers, ok := asSlice(root["servers"])
		if !ok || len(servers) == 0 {
			return "", ""
		}
		server, ok := asMap(servers[0])
		if !ok {
			return "", ""
		}
		rawURL := substituteServerVariables(asString(server["url"]), server["variables"])
		return pathFromServerURL(rawURL), rawURL
	}

	basePath := normalizePath(asString(root["basePath"]))
	host := asString(root["host"])
	if host != "" {
		scheme := "https"
		if schemes := asStringSlice(root["schemes"]); len(schemes) > 0 {
			scheme = schemes[0]
		}
		return basePath, scheme + "://" + host + basePath
	}
	return basePath, ""
}

func substituteServerVariables(raw string, variables interface{}) string {
	ret := raw
	values, ok := asMap(variables)
	if !ok {
		return ret
	}
	for name, rawVariable := range values {
		variable, ok := asMap(rawVariable)
		if !ok {
			continue
		}
		value := asString(variable["default"])
		if value == "" {
			if enum := asStringSlice(variable["enum"]); len(enum) > 0 {
				value = enum[0]
			}
		}
		if value != "" {
			ret = strings.Replace(ret, "{"+name+"}", value, -1)
		}
	}
	return ret
}

func pathFromServerURL(raw string) string {
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.Path != "" {
		return normalizePath(parsed.Path)
	}
	if err == nil && parsed.Host == "" && strings.HasPrefix(raw, "/") {
		return normalizePath(parsed.Path)
	}

	// Relative server values such as "api/v1" are common in hand-written specs.
	if !strings.Contains(raw, "://") {
		if idx := strings.Index(raw, "/"); idx >= 0 {
			return normalizePath(raw[idx:])
		}
	}
	return ""
}

func normalizePath(value string) string {
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return strings.TrimRight(value, "/")
}

func joinPaths(prefix, path string) string {
	prefix = normalizePath(prefix)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if prefix == "" {
		return path
	}
	if path == "/" {
		return prefix + "/"
	}
	return prefix + path
}
