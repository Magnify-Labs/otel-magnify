package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	gitImportTimeout = 30 * time.Second
	gitFileSizeLimit = 1 << 20
)

var (
	gitImportConfig       = importGitConfig
	allowPrivateGitURLs   = false
	errUnsafeGitURL       = errors.New("unsafe git URL")
	errGitFileTooLarge    = errors.New("git file exceeds size limit")
	errInvalidGitPath     = errors.New("invalid git path")
	errInvalidGitRef      = errors.New("invalid git ref")
	gitCommitSHARegexp    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	gitRefRegexp          = regexp.MustCompile(`^[A-Za-z0-9._/@+-]+$`)
	scpLikeGitURLHostExpr = regexp.MustCompile(`^[^@\s]+@([^:\s]+):(.+)$`)
)

type gitImportRequest struct {
	Name    string `json:"name"`
	GitURL  string `json:"git_url"`
	GitRef  string `json:"git_ref"`
	GitPath string `json:"git_path"`
}

type gitImportResult struct {
	Content     string
	CommitSHA   string
	GitURL      string
	GitProvider string
	ImportedAt  time.Time
}

func importGitConfig(ctx context.Context, req gitImportRequest) (gitImportResult, error) {
	if err := validateGitImportRequest(req); err != nil {
		return gitImportResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, gitImportTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "otel-magnify-git-import-*")
	if err != nil {
		return gitImportResult{}, fmt.Errorf("create temp repo: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := runGit(ctx, dir, "init", "--quiet"); err != nil {
		return gitImportResult{}, err
	}
	ref := strings.TrimSpace(req.GitRef)
	if ref == "" {
		ref = "HEAD"
	}
	if err := appendGitRemoteConfig(dir, req.GitURL, ref); err != nil {
		return gitImportResult{}, err
	}
	if err := runGit(ctx, dir, "fetch", "--depth=1", "origin"); err != nil {
		return gitImportResult{}, err
	}
	commitBytes, err := gitOutput(ctx, dir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return gitImportResult{}, err
	}
	commitSHA := strings.TrimSpace(string(commitBytes))
	if !gitCommitSHARegexp.MatchString(commitSHA) {
		return gitImportResult{}, fmt.Errorf("resolved invalid commit SHA %q", commitSHA)
	}
	if err := runGit(ctx, dir, "checkout", "--detach", "--quiet", "FETCH_HEAD"); err != nil {
		return gitImportResult{}, err
	}

	filePath := filepath.Join(dir, filepath.FromSlash(req.GitPath))
	info, err := os.Stat(filePath)
	if err != nil {
		return gitImportResult{}, fmt.Errorf("stat git file: %w", err)
	}
	if info.Size() > gitFileSizeLimit {
		return gitImportResult{}, errGitFileTooLarge
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return gitImportResult{}, fmt.Errorf("read git file: %w", err)
	}
	if len(content) > gitFileSizeLimit {
		return gitImportResult{}, errGitFileTooLarge
	}

	sanitized := sanitizeGitURL(req.GitURL)
	return gitImportResult{
		Content:     string(content),
		CommitSHA:   commitSHA,
		GitURL:      sanitized,
		GitProvider: gitProviderFromURL(sanitized),
		ImportedAt:  time.Now().UTC(),
	}, nil
}

func appendGitRemoteConfig(dir, gitURL, ref string) error {
	configPath := filepath.Join(dir, ".git", "config")
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open git config: %w", err)
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "\n[remote \"origin\"]\n\turl = %s\n\tfetch = +%s:refs/remotes/origin/hermes-import\n", gitURL, ref)
	if err != nil {
		return fmt.Errorf("write git config: %w", err)
	}
	return nil
}

func validateGitImportRequest(req gitImportRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.GitURL) == "" || strings.TrimSpace(req.GitPath) == "" {
		return fmt.Errorf("name, git_url, and git_path are required")
	}
	if err := validateGitPath(req.GitPath); err != nil {
		return err
	}
	if err := validateGitRef(req.GitRef); err != nil {
		return err
	}
	host, err := gitURLHost(req.GitURL)
	if err != nil {
		return err
	}
	if shouldAllowPrivateGitURLs() {
		return nil
	}
	return rejectPrivateGitHost(host)
}

func validateGitPath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\x00") || strings.Contains(p, `\`) {
		return errInvalidGitPath
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return errInvalidGitPath
		}
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return errInvalidGitPath
	}
	return nil
}

func validateGitRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, "-") || strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.HasSuffix(ref, ".lock") {
		return errInvalidGitRef
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.Contains(ref, `\`) || strings.Contains(ref, ":") || strings.Contains(ref, "*") || strings.Contains(ref, "?") || strings.Contains(ref, "[") {
		return errInvalidGitRef
	}
	if !gitRefRegexp.MatchString(ref) {
		return errInvalidGitRef
	}
	return nil
}

func gitURLHost(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != raw || strings.ContainsAny(trimmed, "\x00\r\n\t ") {
		return "", fmt.Errorf("%w: URL must not contain whitespace or control characters", errUnsafeGitURL)
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
		return "", fmt.Errorf("%w: local paths are not allowed", errUnsafeGitURL)
	}
	u, err := url.Parse(trimmed)
	if err == nil && u.Scheme != "" {
		if u.Host == "" {
			return "", fmt.Errorf("%w: absolute http(s) or ssh Git URL required", errUnsafeGitURL)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ssh", "git":
			return u.Hostname(), nil
		default:
			return "", fmt.Errorf("%w: unsupported scheme %q", errUnsafeGitURL, u.Scheme)
		}
	}
	if match := scpLikeGitURLHostExpr.FindStringSubmatch(trimmed); len(match) == 3 {
		return match[1], nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %w", errUnsafeGitURL, err)
	}
	return "", fmt.Errorf("%w: absolute http(s) or ssh Git URL required", errUnsafeGitURL)
}

func rejectPrivateGitHost(host string) error {
	if host == "" {
		return fmt.Errorf("%w: empty host", errUnsafeGitURL)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve git host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve git host: no addresses")
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok || !addr.IsValid() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsPrivate() || addr.IsUnspecified() || addr.IsMulticast() {
			return fmt.Errorf("%w: host resolves to non-public address", errUnsafeGitURL)
		}
	}
	return nil
}

func sanitizeGitURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		return u.String()
	}
	if match := scpLikeGitURLHostExpr.FindStringSubmatch(trimmed); len(match) == 3 {
		return "git@" + match[1] + ":" + match[2]
	}
	if err != nil {
		return raw
	}
	return trimmed
}

func gitProviderFromURL(raw string) string {
	host, err := gitURLHost(raw)
	if err != nil {
		return ""
	}
	host = strings.ToLower(host)
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return "github"
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		return "gitlab"
	default:
		return "generic"
	}
}

func shouldAllowPrivateGitURLs() bool {
	return allowPrivateGitURLs || strings.EqualFold(os.Getenv("OTEL_MAGNIFY_ALLOW_PRIVATE_GIT_URLS"), "true")
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)
	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	args = append([]string{"-c", "http.followRedirects=false"}, args...)
	//nolint:gosec // git is invoked directly without a shell and only with fixed internal subcommands.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
