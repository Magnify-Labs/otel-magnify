package version

import "testing"

func TestCompareSemanticVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
		ok   bool
	}{
		{name: "minor segment numeric ordering", a: "0.9.0", b: "0.10.0", want: -1, ok: true},
		{name: "leading v is ignored", a: "v0.100.0", b: "0.100.0", want: 0, ok: true},
		{name: "release is greater than prerelease", a: "1.0.0", b: "1.0.0-rc.1", want: 1, ok: true},
		{name: "invalid is unknown", a: "not-a-version", b: "1.0.0", want: 0, ok: false},
		{name: "empty is unknown", a: "", b: "1.0.0", want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Compare(tt.a, tt.b)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("Compare(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
			}
		})
	}
}
