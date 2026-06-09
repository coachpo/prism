package registry

import (
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type OperationModelBindingSource string

const (
	OperationModelBindingBody OperationModelBindingSource = "body"
	OperationModelBindingPath OperationModelBindingSource = "path"
)

type Operation struct {
	Name               string
	Method             string
	APIFamily          string
	PathTemplate       string
	Streaming          bool
	ModelBindingSource OperationModelBindingSource
	HookCollectionID   string
}

type OperationDefinition struct {
	Operation   Operation
	PathMatcher OperationPathMatcher
}

type OperationPathMatcher struct {
	staticPath string
	prefix     string
	suffix     string
	paramName  string
}

type OperationMatch struct {
	Operation  Operation
	PathParams map[string]string
}

type AllowedMethods []string

type OperationRegistry interface {
	Resolve(method string, requestPath string) (Operation, AllowedMethods, bool)
}

type InMemoryOperationRegistry struct {
	definitions []OperationDefinition
}

func StaticOperationPath(path string) OperationPathMatcher {
	return OperationPathMatcher{staticPath: strings.TrimSpace(path)}
}

func ParameterizedOperationPath(prefix string, suffix string, paramName string) OperationPathMatcher {
	return OperationPathMatcher{prefix: strings.TrimSpace(prefix), suffix: strings.TrimSpace(suffix), paramName: strings.TrimSpace(paramName)}
}

func NewOperationRegistry(definitions []OperationDefinition) (*InMemoryOperationRegistry, error) {
	issues := validateOperationDefinitions(definitions)
	if len(issues) > 0 {
		return nil, newValidationError("OperationRegistry", issues)
	}
	cloned := make([]OperationDefinition, len(definitions))
	copy(cloned, definitions)
	return &InMemoryOperationRegistry{definitions: cloned}, nil
}

func MustNewOperationRegistry(definitions []OperationDefinition) *InMemoryOperationRegistry {
	registry, err := NewOperationRegistry(definitions)
	if err != nil {
		panic(err)
	}
	return registry
}

func (registry *InMemoryOperationRegistry) Operations() []Operation {
	if registry == nil || len(registry.definitions) == 0 {
		return nil
	}
	operations := make([]Operation, 0, len(registry.definitions))
	for _, definition := range registry.definitions {
		operations = append(operations, definition.Operation)
	}
	return operations
}

func (registry *InMemoryOperationRegistry) Resolve(method string, requestPath string) (Operation, AllowedMethods, bool) {
	match, allowed, ok := registry.ResolveMatch(method, requestPath)
	return match.Operation, allowed, ok
}

func (registry *InMemoryOperationRegistry) ResolveMatch(method string, requestPath string) (OperationMatch, AllowedMethods, bool) {
	if registry == nil {
		return OperationMatch{}, nil, false
	}
	allowed := make(AllowedMethods, 0, 1)
	seenAllowed := map[string]struct{}{}
	for _, definition := range registry.definitions {
		params, matchedPath := definition.PathMatcher.Match(requestPath)
		if !matchedPath {
			continue
		}
		if method == definition.Operation.Method {
			return OperationMatch{Operation: definition.Operation, PathParams: clonePathParams(params)}, nil, true
		}
		if _, seen := seenAllowed[definition.Operation.Method]; !seen {
			seenAllowed[definition.Operation.Method] = struct{}{}
			allowed = append(allowed, definition.Operation.Method)
		}
	}
	sort.Strings(allowed)
	return OperationMatch{}, allowed, false
}

func (matcher OperationPathMatcher) Match(requestPath string) (map[string]string, bool) {
	if matcher.staticPath != "" {
		return nil, requestPath == matcher.staticPath
	}
	if matcher.prefix == "" || matcher.suffix == "" || matcher.paramName == "" {
		return nil, false
	}
	if !strings.HasPrefix(requestPath, matcher.prefix) || !strings.HasSuffix(requestPath, matcher.suffix) {
		return nil, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(requestPath, matcher.prefix), matcher.suffix)
	if value == "" || strings.ContainsAny(value, "/:") {
		return nil, false
	}
	return map[string]string{matcher.paramName: value}, true
}

func validateOperationDefinitions(definitions []OperationDefinition) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if len(definitions) == 0 {
		return append(issues, issue("operation_registry_empty", "operations", "operation registry must contain at least one operation"))
	}
	seenNames := map[string]struct{}{}
	seenRoutes := map[string]struct{}{}
	for index, definition := range definitions {
		field := func(name string) string { return "operations[" + strconv.Itoa(index) + "]." + name }
		issues = append(issues, validateOperation(definition.Operation, field)...)
		issues = append(issues, validateOperationMatcher(definition.PathMatcher, field("path_matcher"))...)
		name := strings.TrimSpace(definition.Operation.Name)
		if name != "" {
			if _, exists := seenNames[name]; exists {
				issues = append(issues, issue("operation_name_duplicate", field("name"), "operation name must be unique"))
			}
			seenNames[name] = struct{}{}
		}
		routeKey := strings.TrimSpace(definition.Operation.Method) + " " + strings.TrimSpace(definition.Operation.PathTemplate)
		if strings.TrimSpace(definition.Operation.Method) != "" && strings.TrimSpace(definition.Operation.PathTemplate) != "" {
			if _, exists := seenRoutes[routeKey]; exists {
				issues = append(issues, issue("operation_route_duplicate", field("path_template"), "operation method and path template must be unique"))
			}
			seenRoutes[routeKey] = struct{}{}
		}
	}
	return issues
}

func validateOperation(operation Operation, field func(string) string) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "operation_name_empty", field("name"), "operation name is required", operation.Name)
	if strings.TrimSpace(operation.Method) != http.MethodPost {
		issues = append(issues, issue("operation_method_invalid", field("method"), "runtime operations must use POST"))
	}
	issues = appendBlankIssue(issues, "operation_api_family_empty", field("api_family"), "api family is required", operation.APIFamily)
	issues = appendBlankIssue(issues, "operation_path_template_empty", field("path_template"), "path template is required", operation.PathTemplate)
	switch operation.ModelBindingSource {
	case OperationModelBindingBody, OperationModelBindingPath:
	default:
		issues = append(issues, issue("operation_model_binding_source_invalid", field("model_binding_source"), "model binding source must be body or path"))
	}
	issues = appendBlankIssue(issues, "operation_hook_collection_empty", field("hook_collection_id"), "hook collection id is required", operation.HookCollectionID)
	return issues
}

func validateOperationMatcher(matcher OperationPathMatcher, field string) []ValidationIssue {
	if matcher.staticPath != "" {
		return nil
	}
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "operation_matcher_prefix_empty", field+".prefix", "parameterized matcher prefix is required", matcher.prefix)
	issues = appendBlankIssue(issues, "operation_matcher_suffix_empty", field+".suffix", "parameterized matcher suffix is required", matcher.suffix)
	issues = appendBlankIssue(issues, "operation_matcher_param_empty", field+".param_name", "parameterized matcher param name is required", matcher.paramName)
	return issues
}

func clonePathParams(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	maps.Copy(cloned, source)
	return cloned
}
