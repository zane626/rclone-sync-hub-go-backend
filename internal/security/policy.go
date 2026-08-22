package security

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ResourcePolicy confines filesystem access and rclone destinations to configured allowlists.
type ResourcePolicy struct {
	enforce        bool
	localRoots     []string
	allowedRemotes map[string]struct{}
}

func NewResourcePolicy(enforce bool, roots, remotes []string) (*ResourcePolicy, error) {
	policy := &ResourcePolicy{enforce: enforce, allowedRemotes: make(map[string]struct{})}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absolute, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			return nil, fmt.Errorf("resolve allowed local root %q: %w", root, err)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			if !enforce {
				continue
			}
			return nil, fmt.Errorf("resolve allowed local root symlinks %q: %w", root, err)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			if !enforce {
				continue
			}
			return nil, fmt.Errorf("allowed local root is not a readable directory: %s", root)
		}
		policy.localRoots = append(policy.localRoots, filepath.Clean(resolved))
	}
	for _, remote := range remotes {
		remote = strings.TrimSpace(remote)
		if remote != "" {
			policy.allowedRemotes[remote] = struct{}{}
		}
	}
	if enforce && len(policy.localRoots) == 0 {
		return nil, errors.New("at least one ALLOWED_LOCAL_ROOTS entry is required when authentication is enabled")
	}
	if enforce && len(policy.allowedRemotes) == 0 {
		return nil, errors.New("at least one rclone remote must be configured when authentication is enabled")
	}
	return policy, nil
}

// ResolveAllowedRemotes returns the effective remote policy. When no explicit
// allowlist is configured, every remote discovered from rclone.conf is allowed.
// An explicit allowlist remains supported for deployments that need to expose
// only a subset of the configured remotes.
func ResolveAllowedRemotes(explicit, discovered []string) ([]string, error) {
	available := make(map[string]struct{}, len(discovered))
	normalizedDiscovered := make([]string, 0, len(discovered))
	for _, name := range discovered {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := available[name]; exists {
			continue
		}
		available[name] = struct{}{}
		normalizedDiscovered = append(normalizedDiscovered, name)
	}
	if len(explicit) == 0 {
		return normalizedDiscovered, nil
	}

	allowed := make([]string, 0, len(explicit))
	seen := make(map[string]struct{}, len(explicit))
	missing := make([]string, 0)
	for _, name := range explicit {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		if _, exists := available[name]; !exists {
			missing = append(missing, name)
			continue
		}
		allowed = append(allowed, name)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("allowed rclone remotes are missing from rclone.conf: %s", strings.Join(missing, ", "))
	}
	return allowed, nil
}

func (p *ResourcePolicy) ValidateLocalFile(input string) (string, error) {
	return p.validateLocal(input, false)
}

func (p *ResourcePolicy) ValidateLocalDirectory(input string) (string, error) {
	return p.validateLocal(input, true)
}

func (p *ResourcePolicy) ValidateRemote(remoteName, remotePath string) (string, error) {
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" || len(remoteName) > 255 || strings.ContainsAny(remoteName, ":\x00\r\n") {
		return "", errors.New("invalid rclone remote name")
	}
	if p != nil && p.enforce {
		if _, ok := p.allowedRemotes[remoteName]; !ok {
			return "", fmt.Errorf("rclone remote %q is not allowed", remoteName)
		}
	}
	if len(remotePath) == 0 || len(remotePath) > 1024 || strings.ContainsAny(remotePath, "\x00\r\n") {
		return "", errors.New("invalid remote path")
	}
	normalized := path.Clean(strings.TrimPrefix(strings.ReplaceAll(remotePath, "\\", "/"), "/"))
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", errors.New("remote path traversal is not allowed")
	}
	return normalized, nil
}

func (p *ResourcePolicy) validateLocal(input string, requireDirectory bool) (string, error) {
	if strings.TrimSpace(input) == "" || len(input) > 4096 || strings.ContainsRune(input, '\x00') {
		return "", errors.New("invalid local path")
	}
	absolute, err := filepath.Abs(filepath.Clean(input))
	if err != nil {
		return "", fmt.Errorf("resolve local path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve local path symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat local path: %w", err)
	}
	if requireDirectory && !info.IsDir() {
		return "", errors.New("local path must be a directory")
	}
	if !requireDirectory && !info.Mode().IsRegular() {
		return "", errors.New("local path must be a regular file")
	}
	resolved = filepath.Clean(resolved)
	if p != nil && p.enforce && !p.withinAllowedRoot(resolved) {
		return "", errors.New("local path is outside the allowed roots")
	}
	return resolved, nil
}

func (p *ResourcePolicy) withinAllowedRoot(candidate string) bool {
	for _, root := range p.localRoots {
		relative, err := filepath.Rel(root, candidate)
		if err != nil {
			continue
		}
		if relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (p *ResourcePolicy) AllowedLocalRoots() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.localRoots...)
}

func (p *ResourcePolicy) AllowsRemote(name string) bool {
	if p == nil || !p.enforce {
		return true
	}
	_, ok := p.allowedRemotes[name]
	return ok
}
