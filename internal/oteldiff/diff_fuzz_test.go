package oteldiff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

const maxDiffFuzzInputBytes = 64 << 10

func TestFuzzRedactionValuesAreOpaque(t *testing.T) {
	values := fuzzRedactionValues("base", []byte("fixture"))
	assertFuzzRedactionValuesAreOpaque(t, values)

	authorization := "Custom " + values.authorization
	if looksSecret(authorization) {
		t.Fatalf("generated authorization must not satisfy looksSecret: %q", authorization)
	}
	if looksSecret("credentials") {
		t.Fatal("endpoint query key must not satisfy looksSecret")
	}
	if bytes.Contains(fuzzConfigWithRedactionValues(values), []byte("Bearer ")) {
		t.Fatal("generated authorization must not use the Bearer scheme")
	}
}

func FuzzCompare(f *testing.F) {
	base := loadFuzzFixture(f, "base.yaml")

	for _, targetName := range []string{
		"base.yaml",
		"low-target.yaml",
		"medium-target.yaml",
		"high-target.yaml",
	} {
		f.Add(base, loadFuzzFixture(f, targetName))
	}

	f.Add(
		loadFuzzFixture(f, "auth-base.yaml"),
		loadFuzzFixture(f, "auth-target.yaml"),
	)
	f.Add(
		loadFuzzFixture(f, "sampling-base.yaml"),
		loadFuzzFixture(f, "sampling-target.yaml"),
	)
	f.Add([]byte{}, []byte{})
	f.Add([]byte("receivers: [unterminated"), base)

	f.Fuzz(func(t *testing.T, baseYAML, targetYAML []byte) {
		if len(baseYAML) > maxDiffFuzzInputBytes || len(targetYAML) > maxDiffFuzzInputBytes {
			return
		}

		first := Compare(baseYAML, targetYAML)
		firstJSON := assertConfigDiffInvariants(t, first)

		second := Compare(baseYAML, targetYAML)
		secondJSON := assertConfigDiffInvariants(t, second)

		if !bytes.Equal(firstJSON, secondJSON) {
			t.Fatalf("Compare is not deterministic:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
		}

		baseValues := fuzzRedactionValues("base", baseYAML)
		targetValues := fuzzRedactionValues("target", targetYAML)
		assertFuzzRedactionValuesAreOpaque(t, baseValues, targetValues)
		redacted := Compare(
			fuzzConfigWithRedactionValues(baseValues),
			fuzzConfigWithRedactionValues(targetValues),
		)
		redactedJSON := assertConfigDiffInvariants(t, redacted)

		if !redacted.Valid {
			t.Fatalf("generated redaction configs must be valid: %#v", redacted.Diagnostics)
		}
		for _, values := range []fuzzRedactionValueSet{baseValues, targetValues} {
			for _, value := range values.all() {
				if bytes.Contains(redactedJSON, []byte(value)) {
					t.Fatalf("diff leaked redaction value %q: %s", value, redactedJSON)
				}
			}
		}
		if !bytes.Contains(redactedJSON, []byte(MaskedValue)) {
			t.Fatalf("redacted diff contains no masked sentinel: %s", redactedJSON)
		}
	})
}

func loadFuzzFixture(f *testing.F, name string) []byte {
	f.Helper()

	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		f.Fatalf("read fuzz fixture %s: %v", name, err)
	}
	return data
}

func assertConfigDiffInvariants(t *testing.T, diff ConfigDiff) []byte {
	t.Helper()

	if diff.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %q, want %q", diff.SchemaVersion, SchemaVersion)
	}
	if diff.BlastRadius.SchemaVersion != BlastRadiusSchemaVersion {
		t.Fatalf(
			"blast radius schema version = %q, want %q",
			diff.BlastRadius.SchemaVersion,
			BlastRadiusSchemaVersion,
		)
	}
	if diff.RiskScore.Severity != string(diff.Summary.OverallRisk) {
		t.Fatalf(
			"risk score severity = %q, summary risk = %q",
			diff.RiskScore.Severity,
			diff.Summary.OverallRisk,
		)
	}
	if diff.Valid != (len(diff.Diagnostics) == 0) {
		t.Fatalf("valid = %t with %d diagnostics", diff.Valid, len(diff.Diagnostics))
	}

	if diff.HumanSummary == nil ||
		diff.Components == nil ||
		diff.Pipelines == nil ||
		diff.Endpoints == nil ||
		diff.Security == nil ||
		diff.RiskItems == nil ||
		diff.Diagnostics == nil ||
		diff.RiskScore.Reasons == nil ||
		diff.BlastRadius.AffectedSignals == nil ||
		diff.BlastRadius.TouchedExporters == nil ||
		diff.BlastRadius.ImpactedServices == nil ||
		diff.BlastRadius.ImpactedClusters == nil ||
		diff.BlastRadius.CriticalCollectors == nil {
		t.Fatalf("schema collections must be non-nil: %#v", diff)
	}

	switch diff.Summary.OverallRisk {
	case RiskNone, RiskLow, RiskMedium, RiskHigh:
	default:
		t.Fatalf("unknown overall risk %q", diff.Summary.OverallRisk)
	}

	body, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("marshal config diff: %v", err)
	}
	return body
}

type fuzzRedactionValueSet struct {
	endpointUserinfo string
	endpointQuery    string
	authorization    string
	apiKey           string
	password         string
}

func (v fuzzRedactionValueSet) all() []string {
	return []string{v.endpointUserinfo, v.endpointQuery, v.authorization, v.apiKey, v.password}
}

func fuzzRedactionValues(side string, input []byte) fuzzRedactionValueSet {
	return fuzzRedactionValueSet{
		endpointUserinfo: fuzzOpaqueValue(side, 1, input),
		endpointQuery:    fuzzOpaqueValue(side, 2, input),
		authorization:    fuzzOpaqueValue(side, 3, input),
		apiKey:           fuzzOpaqueValue(side, 4, input),
		password:         fuzzOpaqueValue(side, 5, input),
	}
}

func fuzzOpaqueValue(side string, location int, input []byte) string {
	sum := sha256.Sum256(append([]byte(side), input...))
	sidePrefix := "a"
	if side == "target" {
		sidePrefix = "b"
	}
	return fmt.Sprintf("v%s%d%s", sidePrefix, location, hex.EncodeToString(sum[:]))
}

func assertFuzzRedactionValuesAreOpaque(t *testing.T, valueSets ...fuzzRedactionValueSet) {
	t.Helper()

	seen := make(map[string]struct{})
	for _, values := range valueSets {
		for _, value := range values.all() {
			if looksSecret(value) {
				t.Fatalf("generated redaction value must not satisfy looksSecret: %q", value)
			}
			if _, ok := seen[value]; ok {
				t.Fatalf("generated redaction values must be distinct: %q", value)
			}
			seen[value] = struct{}{}
		}
	}
}

func fuzzConfigWithRedactionValues(values fuzzRedactionValueSet) []byte {
	return []byte(fmt.Sprintf(`
receivers:
  otlp: {}
processors:
  batch: {}
exporters:
  otlp:
    endpoint: https://collector:%s@telemetry.example:4317/v1/traces?credentials=%s
    headers:
      Authorization: Custom %s
      x-api-key: %s
    auth:
      username: collector
      password: %s
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp]
`,
		values.endpointUserinfo,
		values.endpointQuery,
		values.authorization,
		values.apiKey,
		values.password,
	))
}
