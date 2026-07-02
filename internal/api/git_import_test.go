package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitOutputDisablesHTTPRedirects(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "git-args.txt")
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nprintf '%s\n' \"$@\" > \"$GIT_ARGS_FILE\"\n"), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)

	if _, err := gitOutput(context.Background(), t.TempDir(), "fetch", "origin", "main"); err != nil {
		t.Fatalf("gitOutput: %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	wantPrefix := []string{"-c", "http.followRedirects=false", "fetch"}
	if len(got) < len(wantPrefix) {
		t.Fatalf("git args = %v, want prefix %v", got, wantPrefix)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("git args = %v, want prefix %v", got, wantPrefix)
		}
	}
}

func TestValidateGitPathRejectsParentSegments(t *testing.T) {
	for _, p := range []string{"../collector.yaml", "configs/../collector.yaml", "configs/../../collector.yaml"} {
		if err := validateGitPath(p); err == nil {
			t.Fatalf("validateGitPath(%q) = nil, want error", p)
		}
	}
}

func TestSanitizeGitURLPreservesHTTPSPortWhileStrippingCredentials(t *testing.T) {
	got := sanitizeGitURL("https://token:secret@git.example.com:8443/acme/collectors.git")
	want := "https://git.example.com:8443/acme/collectors.git"
	if got != want {
		t.Fatalf("sanitizeGitURL() = %q, want %q", got, want)
	}
}

func TestValidateGitRefRejectsOptionLikeRef(t *testing.T) {
	if err := validateGitRef("--upload-pack=/tmp/evil"); err == nil {
		t.Fatal("validateGitRef accepted option-like ref")
	}
}
