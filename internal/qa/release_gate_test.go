package qa_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreTagGateIsDocumentedAndRunsBenchmarkSmoke(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	ci := readText(t, filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	releaseDocs := readText(t, filepath.Join(repoRoot, "docs", "developers", "release.md"))
	preTagGate := readText(t, filepath.Join(repoRoot, "scripts", "pre-tag-gate.sh"))
	testingDocs := readText(t, filepath.Join(repoRoot, "docs", "developers", "testing.md"))
	validatorTests := readText(t, filepath.Join(repoRoot, "internal", "validator", "validator_test.go"))

	for _, check := range []struct {
		name string
		got  string
		want string
	}{
		{
			name: "ci invokes the pre-tag gate script without creating tags",
			got:  ci,
			want: "bash scripts/pre-tag-gate.sh",
		},
		{
			name: "release docs list the local pre-tag gate command",
			got:  releaseDocs,
			want: "bash scripts/pre-tag-gate.sh",
		},
		{
			name: "testing docs document the targeted benchmark guardrail",
			got:  testingDocs,
			want: "go test -run '^$' -bench '^BenchmarkValidateMinimalConfig$' -benchmem ./internal/validator",
		},
		{
			name: "validator package defines the benchmark used by the guardrail",
			got:  validatorTests,
			want: "func BenchmarkValidateMinimalConfig",
		},
		{
			name: "pre-tag gate runs the targeted benchmark smoke",
			got:  preTagGate,
			want: "BenchmarkValidateMinimalConfig",
		},
	} {
		if !strings.Contains(check.got, check.want) {
			t.Fatalf("%s: missing %q", check.name, check.want)
		}
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
