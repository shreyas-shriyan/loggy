// Package awsauth loads AWS profiles and performs IAM Identity Center (SSO) sign-in.
package awsauth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Identity describes the caller returned by STS.
type Identity struct {
	Account string
	ARN     string
}

// Load builds an AWS config for profile and region. Empty values use SDK defaults.
func Load(ctx context.Context, profile, region string) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// Check verifies the credentials in cfg with STS GetCallerIdentity.
func Check(ctx context.Context, cfg aws.Config) (Identity, error) {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return Identity{}, err
	}
	return Identity{Account: aws.ToString(out.Account), ARN: aws.ToString(out.Arn)}, nil
}

// Session is a verified AWS config.
type Session struct {
	Config   aws.Config
	Identity Identity
	Profile  string
}

// Ensure returns verified credentials for profile, signing in through SSO when
// the profile supports it and confirm approves.
func Ensure(ctx context.Context, profile, region string, out io.Writer, confirm func(prompt string) bool) (*Session, error) {
	cfg, err := Load(ctx, profile, region)
	if err != nil {
		return nil, fmt.Errorf("load AWS profile %q: %w", profile, err)
	}
	id, checkErr := Check(ctx, cfg)
	if checkErr == nil {
		return &Session{Config: cfg, Identity: id, Profile: profile}, nil
	}

	sso, err := ssoSettings(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("AWS credentials for profile %q are not valid: %w", profile, checkErr)
	}
	if sso == nil {
		return nil, fmt.Errorf("AWS credentials for profile %q are not valid (%v); this profile does not use SSO, so refresh it outside loggy (for example `aws login --profile %s`)", profile, checkErr, profile)
	}
	if !confirm(fmt.Sprintf("Your SSO session for %q has expired. Sign in now in the browser?", displayProfile(profile))) {
		return nil, errors.New("sign-in cancelled")
	}
	if err := Login(ctx, *sso, out); err != nil {
		return nil, err
	}

	cfg, err = Load(ctx, profile, region)
	if err != nil {
		return nil, err
	}
	id, err = Check(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("still not signed in after SSO login: %w", err)
	}
	return &Session{Config: cfg, Identity: id, Profile: profile}, nil
}

func displayProfile(p string) string {
	if p == "" {
		return "default"
	}
	return p
}

// ssoSettings returns the SSO settings of profile, or nil if it does not use SSO.
func ssoSettings(ctx context.Context, profile string) (*SSOSettings, error) {
	name := profile
	if name == "" {
		name = "default"
	}
	sc, err := awsconfig.LoadSharedConfigProfile(ctx, name, sharedFiles)
	if err != nil {
		return nil, err
	}
	if sc.SSOSession != nil && sc.SSOSession.SSOStartURL != "" {
		return &SSOSettings{
			SessionName: sc.SSOSession.Name,
			StartURL:    sc.SSOSession.SSOStartURL,
			Region:      sc.SSOSession.SSORegion,
		}, nil
	}
	if sc.SSOStartURL != "" {
		return &SSOSettings{StartURL: sc.SSOStartURL, Region: sc.SSORegion}, nil
	}
	return nil, nil
}

// ListProfiles returns profile names from the shared AWS config and credentials files.
func ListProfiles() []string {
	seen := map[string]bool{}
	for _, s := range readSections(ConfigFile()) {
		switch {
		case s.name == "default":
			seen[s.name] = true
		case strings.HasPrefix(s.name, "profile "):
			seen[strings.TrimSpace(strings.TrimPrefix(s.name, "profile "))] = true
		}
	}
	for _, s := range readSections(CredentialsFile()) {
		seen[s.name] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ConfigFile returns the path of the shared AWS config file.
func ConfigFile() string { return sharedFile("AWS_CONFIG_FILE", "config") }

// CredentialsFile returns the path of the shared AWS credentials file.
func CredentialsFile() string { return sharedFile("AWS_SHARED_CREDENTIALS_FILE", "credentials") }

// sharedFiles makes LoadSharedConfigProfile honour AWS_CONFIG_FILE and
// AWS_SHARED_CREDENTIALS_FILE, which it ignores by default.
func sharedFiles(o *awsconfig.LoadSharedConfigOptions) {
	o.ConfigFiles = []string{ConfigFile()}
	o.CredentialsFiles = []string{CredentialsFile()}
}

func sharedFile(envVar, name string) string {
	if p := os.Getenv(envVar); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", name)
}

type section struct {
	name string
	keys map[string]string
}

// readSections parses an INI file into sections; a missing file has none.
func readSections(path string) []section {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []section
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			out = append(out, section{name: strings.TrimSpace(line[1 : len(line)-1]), keys: map[string]string{}})
		case len(out) > 0 && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";"):
			if k, v, ok := strings.Cut(line, "="); ok {
				out[len(out)-1].keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return out
}
