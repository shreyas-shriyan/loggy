// Package config loads and saves loggy's YAML configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Target types.
const (
	TypeEKS = "eks"
	TypeECS = "ecs"
)

// Environments known to loggy.
var Environments = []string{"dev", "prod"}

// Environment holds per-environment AWS defaults.
type Environment struct {
	Profile    string `yaml:"profile"`
	Region     string `yaml:"region"`
	EKSCluster string `yaml:"eks_cluster,omitempty"`
}

// Target is a saved log source.
type Target struct {
	Name    string `yaml:"name"`
	Env     string `yaml:"env"`
	Type    string `yaml:"type"`
	Profile string `yaml:"profile,omitempty"`
	Region  string `yaml:"region,omitempty"`

	// EKS and ECS
	Cluster   string `yaml:"cluster,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
	Service   string `yaml:"service,omitempty"`

	// ECS
	LogGroup string `yaml:"log_group,omitempty"`
}

// Key identifies a target for de-duplication.
func (t Target) Key() string {
	return strings.Join([]string{t.Env, t.Type, t.Region, t.Cluster, t.Namespace, t.Service, t.LogGroup}, "|")
}

// Supported reports whether loggy can read logs for t.
func (t Target) Supported() bool { return t.Type == TypeEKS || t.Type == TypeECS }

// Config is the full on-disk configuration.
type Config struct {
	Environments map[string]Environment `yaml:"environments"`
	Targets      []Target               `yaml:"targets"`
	// HealthPatterns are regexes for health-check lines hidden by default;
	// empty means the built-in defaults.
	HealthPatterns []string `yaml:"health_patterns,omitempty"`

	path string
}

// DefaultPath returns the config file location, honouring LOGGY_CONFIG.
func DefaultPath() (string, error) {
	if p := os.Getenv("LOGGY_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "loggy", "config.yaml"), nil
}

// LogDir returns the directory for saved log files, honouring LOGGY_LOG_DIR.
func LogDir() string {
	if d := os.Getenv("LOGGY_LOG_DIR"); d != "" {
		return d
	}
	return "logs"
}

// New returns an empty config bound to path.
func New(path string) *Config {
	c := &Config{path: path, Environments: map[string]Environment{}}
	c.normalize()
	return c
}

// Load reads the config at path; a missing file yields an empty config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(path), nil
	}
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.path = path
	c.normalize()
	return c, nil
}

func (c *Config) normalize() {
	if c.Environments == nil {
		c.Environments = map[string]Environment{}
	}
	for _, e := range Environments {
		if _, ok := c.Environments[e]; !ok {
			c.Environments[e] = Environment{}
		}
	}
}

// Path returns the file this config is bound to.
func (c *Config) Path() string { return c.path }

// Save writes the config atomically with 0600 permissions.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	data := buf.Bytes()
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}

// Env returns the settings for env.
func (c *Config) Env(env string) Environment { return c.Environments[env] }

// SetEnv replaces the settings for env.
func (c *Config) SetEnv(env string, e Environment) { c.Environments[env] = e }

// TargetsFor returns the targets saved for env, sorted by name.
func (c *Config) TargetsFor(env string) []Target {
	var out []Target
	for _, t := range c.Targets {
		if t.Env == env {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AddTarget appends t unless an identical target exists; it reports whether t was added.
func (c *Config) AddTarget(t Target) bool {
	for _, existing := range c.Targets {
		if existing.Key() == t.Key() {
			return false
		}
	}
	c.Targets = append(c.Targets, t)
	return true
}

// RemoveTargets deletes every target whose key is in keys and returns the number removed.
func (c *Config) RemoveTargets(keys []string) int {
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	kept := c.Targets[:0]
	removed := 0
	for _, t := range c.Targets {
		if drop[t.Key()] {
			removed++
			continue
		}
		kept = append(kept, t)
	}
	c.Targets = kept
	return removed
}

// ProfileFor resolves the AWS profile for t, falling back to its environment.
func (c *Config) ProfileFor(t Target) string {
	if t.Profile != "" {
		return t.Profile
	}
	return c.Env(t.Env).Profile
}

// RegionFor resolves the AWS region for t, falling back to its environment.
func (c *Config) RegionFor(t Target) string {
	if t.Region != "" {
		return t.Region
	}
	return c.Env(t.Env).Region
}

// ValidEnv reports whether env is a known environment.
func ValidEnv(env string) bool {
	for _, e := range Environments {
		if e == env {
			return true
		}
	}
	return false
}
