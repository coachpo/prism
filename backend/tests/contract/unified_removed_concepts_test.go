package contract_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

type removedConceptScanRoot struct {
	RelPath string
	Glob    bool
}

type removedConceptTerm struct {
	Group           string
	Value           string
	ExactIdentifier bool
}

type removedConceptViolation struct {
	Path  string
	Line  int
	Group string
	Term  string
	Text  string
}

var removedConceptScanRoots = []removedConceptScanRoot{
	{RelPath: "backend/internal"},
	{RelPath: "backend/internal/domain/stats"},
	{RelPath: "frontend/src"},
	{RelPath: "README.md"},
	{RelPath: "backend/README.md"},
	{RelPath: "frontend/README.md"},
	{RelPath: "docs/*.md", Glob: true},
	{RelPath: "AGENTS.md"},
	{RelPath: "backend/AGENTS.md"},
	{RelPath: "frontend/AGENTS.md"},
	{RelPath: "config.json"},
}

var unifiedRemovedConceptTerms = []removedConceptTerm{
	removedConceptIdentifier("unified model access", "model_type"),
	removedConceptIdentifier("unified model access", "proxy_selection_strategy"),
	removedConceptIdentifier("unified model access", "model_proxy_targets"),
	removedConceptIdentifier("unified model access", "proxy_targets"),
	removedConceptIdentifier("unified model access", "strategy_type"),
	removedConceptIdentifier("unified model access", "routing_policy"),
	removedConceptIdentifier("unified model access", "auto_recovery"),
	removedConceptPhrase("unified model access", "Native/Proxy"),
	removedConceptPhrase("unified model access", "Adaptive Routing"),
	removedConceptPhrase("unified model access", "Auto Recovery"),
	removedConceptIdentifier("unified model access", "ProxyModelDetailPage"),
	removedConceptIdentifier("unified model access", "ProxyTargetsEditor"),
	removedConceptPhrase("unified model access", "/models/:id/proxy"),
	removedConceptIdentifier("unified model access", "is_proxy_origin"),
	removedConceptIdentifier("unified model access", "ModelType"),
	removedConceptIdentifier("unified model access", "ProxySelectionStrategy"),
	removedConceptIdentifier("unified model access", "ProxyTarget"),
	removedConceptIdentifier("unified model access", "LoadbalanceStrategyFamily"),
	removedConceptIdentifier("unified model access", "AdaptiveRoutingObjective"),
	removedConceptIdentifier("unified model access", "LoadbalanceRoutingPolicy"),
	removedConceptIdentifier("unified model access", "LoadbalanceAutoRecovery"),
}

var runtimeBufferingRemovedConceptTerms = []removedConceptTerm{
	removedConceptPhrase("runtime buffering", "runtime.buffering_mode"),
	removedConceptPhrase("runtime buffering", "runtime.bufferingMode"),
	removedConceptIdentifier("runtime buffering", "BufferingMode"),
	removedConceptIdentifier("runtime buffering", "RuntimeBufferingMode"),
	removedConceptPhrase("runtime buffering", "configurable buffering mode"),
	removedConceptPhrase("runtime buffering", "operator-selectable buffering"),
}

var removedConceptAllowedMatchPaths = map[string]struct{}{
	"backend/tests/contract/unified_removed_concepts_test.go":             {},
	"backend/internal/httpapi/management/bootstrapconfig/service_test.go": {},
	"backend/internal/httpapi/management/bootstrapconfig":                 {},
	"backend/internal/httpapi/runtime/runtime_test.go":                    {},
	"backend/internal/httpapi/runtime":                                    {},
	"backend/internal/platform/config/bootstrap_management_test.go":      {},
	"backend/internal/platform/config/bootstrap_apply_test.go":            {},
	"backend/internal/platform/http/hot_bootstrap_runtime_test.go":        {},
	"backend/internal/platform/http":                                      {},
	"backend/tests/integration/bootstrap_config_test.go":                  {},
	"backend/tests/contract/bootstrap_config_contract_test.go":            {},
}

func TestUnifiedRemovedConceptsScan(t *testing.T) {
	repoRoot := removedConceptRepoRoot(t)
	files := removedConceptScanFiles(t, repoRoot)

	assertRemovedConceptScope(t, files)
	assertRemovedConceptGroupActive(t, unifiedRemovedConceptTerms, "model_type")

	violations := removedConceptScanViolations(t, repoRoot, files)
	if len(violations) > 0 {
		t.Fatalf("removed concepts reintroduced in Task 19 scan scope:\n%s", formatRemovedConceptViolations(violations))
	}
}

func TestRuntimeBufferingModeRemovalTerms(t *testing.T) {
	assertRemovedConceptGroupActive(t, runtimeBufferingRemovedConceptTerms, "RuntimeBufferingMode")

	proxyOnly := "runtime proxy handlers, proxy API keys, Vite proxying, and CLIProxyTarget remain legitimate terminology"
	if violations := removedConceptViolationsForContent("proxy-only.txt", proxyOnly); len(violations) != 0 {
		t.Fatalf("proxy terminology outside the forbidden exact-term list must be allowed, got %+v", violations)
	}

	legacyStrategy := "load-balance payloads use legacy_strategy_type for Ban Policy routing"
	if violations := removedConceptViolationsForContent("legacy-strategy.txt", legacyStrategy); len(violations) != 0 {
		t.Fatalf("legacy_strategy_type must be allowed, got %+v", violations)
	}

	bareStrategy := "removed payload key strategy_type must still be rejected"
	if violations := removedConceptViolationsForContent("bare-strategy.txt", bareStrategy); len(violations) != 1 {
		t.Fatalf("bare strategy_type must be forbidden, got %+v", violations)
	}
}

func removedConceptIdentifier(group string, value string) removedConceptTerm {
	return removedConceptTerm{Group: group, Value: value, ExactIdentifier: true}
}

func removedConceptPhrase(group string, value string) removedConceptTerm {
	return removedConceptTerm{Group: group, Value: value}
}

func removedConceptRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func removedConceptScanFiles(t *testing.T, repoRoot string) []string {
	t.Helper()
	seen := map[string]struct{}{}
	for _, root := range removedConceptScanRoots {
		matches := []string{filepath.Join(repoRoot, root.RelPath)}
		if root.Glob {
			globMatches, err := filepath.Glob(filepath.Join(repoRoot, root.RelPath))
			if err != nil {
				t.Fatalf("glob scan root %s: %v", root.RelPath, err)
			}
			matches = globMatches
		}
		if len(matches) == 0 {
			t.Fatalf("scan root %s matched no files", root.RelPath)
		}
		for _, match := range matches {
			addRemovedConceptPath(t, repoRoot, match, seen)
		}
	}

	files := make([]string, 0, len(seen))
	for rel := range seen {
		files = append(files, rel)
	}
	sort.Strings(files)
	return files
}

func addRemovedConceptPath(t *testing.T, repoRoot string, absolutePath string, seen map[string]struct{}) {
	t.Helper()
	info, err := os.Stat(absolutePath)
	if err != nil {
		t.Fatalf("stat scan path %s: %v", absolutePath, err)
	}
	if !info.IsDir() {
		addRemovedConceptFile(t, repoRoot, absolutePath, seen)
		return
	}

	err = filepath.WalkDir(absolutePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := removedConceptRelPath(t, repoRoot, path)
		if entry.IsDir() {
			if rel != "." && removedConceptExcludedPath(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if removedConceptExcludedPath(rel) {
			return nil
		}
		addRemovedConceptFile(t, repoRoot, path, seen)
		return nil
	})
	if err != nil {
		t.Fatalf("walk scan root %s: %v", absolutePath, err)
	}
}

func addRemovedConceptFile(t *testing.T, repoRoot string, absolutePath string, seen map[string]struct{}) {
	t.Helper()
	info, err := os.Stat(absolutePath)
	if err != nil {
		t.Fatalf("stat scan file %s: %v", absolutePath, err)
	}
	if info.IsDir() {
		t.Fatalf("scan file %s is a directory", absolutePath)
	}
	rel := removedConceptRelPath(t, repoRoot, absolutePath)
	if removedConceptExcludedPath(rel) {
		return
	}
	seen[rel] = struct{}{}
}

func removedConceptRelPath(t *testing.T, repoRoot string, absolutePath string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot, absolutePath)
	if err != nil {
		t.Fatalf("rel path for %s: %v", absolutePath, err)
	}
	return filepath.ToSlash(rel)
}

func removedConceptExcludedPath(rel string) bool {
	switch {
	case strings.HasPrefix(rel, ".omo/"):
		return true
	case strings.HasPrefix(rel, "docs/archive/"):
		return true
	case strings.HasPrefix(rel, "frontend/tests/"):
		return true
	case strings.HasPrefix(rel, "backend/tests/") && rel != "backend/tests/contract/unified_removed_concepts_test.go":
		return true
	default:
		return false
	}
}

func removedConceptScanViolations(t *testing.T, repoRoot string, files []string) []removedConceptViolation {
	t.Helper()
	var violations []removedConceptViolation
	for _, rel := range files {
		content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read scan file %s: %v", rel, err)
		}
		if _, ok := removedConceptAllowedMatchPaths[rel]; ok {
			continue
		}
		violations = append(violations, removedConceptViolationsForContent(rel, string(content))...)
	}
	return violations
}

func removedConceptViolationsForContent(rel string, content string) []removedConceptViolation {
	terms := append([]removedConceptTerm{}, unifiedRemovedConceptTerms...)
	terms = append(terms, runtimeBufferingRemovedConceptTerms...)
	var violations []removedConceptViolation
	for lineIndex, line := range strings.Split(content, "\n") {
		for _, term := range terms {
			if !removedConceptTermMatches(line, term) {
				continue
			}
			violations = append(violations, removedConceptViolation{
				Path:  rel,
				Line:  lineIndex + 1,
				Group: term.Group,
				Term:  term.Value,
				Text:  strings.TrimSpace(line),
			})
		}
	}
	return violations
}

func removedConceptTermMatches(line string, term removedConceptTerm) bool {
	if term.ExactIdentifier {
		return containsExactIdentifier(line, term.Value)
	}
	return strings.Contains(line, term.Value)
}

func containsExactIdentifier(line string, identifier string) bool {
	start := 0
	for {
		index := strings.Index(line[start:], identifier)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !isIdentifierByte(line[index-1])
		afterIndex := index + len(identifier)
		afterOK := afterIndex == len(line) || !isIdentifierByte(line[afterIndex])
		if beforeOK && afterOK {
			return true
		}
		start = index + len(identifier)
	}
}

func isIdentifierByte(value byte) bool {
	return value == '_' || value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func assertRemovedConceptScope(t *testing.T, files []string) {
	t.Helper()
	assertRemovedConceptRoots(t)
	assertRemovedConceptFileIncluded(t, files, "config.json")
	assertRemovedConceptDirIncluded(t, files, "backend/internal/domain/stats/")
	for _, rel := range files {
		if removedConceptExcludedPath(rel) {
			t.Fatalf("excluded path %s was included in removed-concepts scan", rel)
		}
	}
}

func assertRemovedConceptRoots(t *testing.T) {
	t.Helper()
	want := []string{
		"backend/internal",
		"backend/internal/domain/stats",
		"frontend/src",
		"README.md",
		"backend/README.md",
		"frontend/README.md",
		"docs/*.md",
		"AGENTS.md",
		"backend/AGENTS.md",
		"frontend/AGENTS.md",
		"config.json",
	}
	got := make([]string, 0, len(removedConceptScanRoots))
	for _, root := range removedConceptScanRoots {
		got = append(got, root.RelPath)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("removed-concepts scan roots changed\nwant: %v\n got: %v", want, got)
	}
}

func assertRemovedConceptFileIncluded(t *testing.T, files []string, rel string) {
	t.Helper()
	if slices.Contains(files, rel) {
		return
	}
	t.Fatalf("removed-concepts scan did not include %s", rel)
}

func assertRemovedConceptDirIncluded(t *testing.T, files []string, dir string) {
	t.Helper()
	for _, file := range files {
		if strings.HasPrefix(file, dir) {
			return
		}
	}
	t.Fatalf("removed-concepts scan did not include %s", dir)
}

func assertRemovedConceptGroupActive(t *testing.T, terms []removedConceptTerm, sampleTerm string) {
	t.Helper()
	for _, term := range terms {
		violations := removedConceptViolationsForContent("sample.txt", fmt.Sprintf("removed term: %s", term.Value))
		if len(violations) == 0 {
			t.Fatalf("removed-concepts matcher did not catch %s term %q", term.Group, term.Value)
		}
	}
	violations := removedConceptViolationsForContent("sample.txt", fmt.Sprintf("removed term: %s", sampleTerm))
	if len(violations) == 0 {
		t.Fatalf("removed-concepts matcher did not catch sample term %q", sampleTerm)
	}
}

func formatRemovedConceptViolations(violations []removedConceptViolation) string {
	var builder strings.Builder
	for _, violation := range violations {
		fmt.Fprintf(&builder, "%s:%d: %s term %q: %s\n", violation.Path, violation.Line, violation.Group, violation.Term, violation.Text)
	}
	return builder.String()
}
