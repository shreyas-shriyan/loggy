package awsauth

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

func useTempAWSFiles(t *testing.T, config string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "aws", "config")
	creds := filepath.Join(dir, "aws", "credentials")
	if config != "" {
		if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AWS_CONFIG_FILE", cfg)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", creds)
	return cfg, creds
}

func TestAddSSOProfileReusesSession(t *testing.T) {
	cfg, _ := useTempAWSFiles(t, "[default]\nregion = us-east-1\n[sso-session corp]\nsso_start_url = https://corp.awsapps.com/start\nsso_region = eu-west-1") // no trailing newline

	sessions := SSOSessions()
	if len(sessions) != 1 || sessions[0] != (SSOSettings{SessionName: "corp", StartURL: "https://corp.awsapps.com/start", Region: "eu-west-1"}) {
		t.Fatalf("sessions = %+v", sessions)
	}
	err := AddSSOProfile(SSOProfile{Name: "corp-dev", Region: "us-west-2", AccountID: "123456789012", RoleName: "ReadOnly", Session: sessions[0]})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	if n := strings.Count(string(data), "[sso-session corp]"); n != 1 {
		t.Fatalf("sso-session written %d times:\n%s", n, data)
	}

	sc, err := awsconfig.LoadSharedConfigProfile(context.Background(), "corp-dev", sharedFiles)
	if err != nil {
		t.Fatal(err)
	}
	if sc.SSOAccountID != "123456789012" || sc.SSORoleName != "ReadOnly" || sc.Region != "us-west-2" || sc.SSOSession == nil || sc.SSOSession.SSORegion != "eu-west-1" {
		t.Fatalf("loaded = %+v", sc)
	}
	if got := ListProfiles(); !slices.Equal(got, []string{"corp-dev", "default"}) {
		t.Fatalf("profiles = %v", got)
	}
}

func TestAddSSOProfileNewSession(t *testing.T) {
	cfg, _ := useTempAWSFiles(t, "")
	s := SSOSettings{SessionName: "acme", StartURL: "https://acme.awsapps.com/start", Region: "us-east-1"}
	if err := AddSSOProfile(SSOProfile{Name: "acme-prod", Region: "us-west-2", AccountID: "111122223333", RoleName: "Admin", Session: s}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	got, err := ssoSettings(context.Background(), "acme-prod")
	if err != nil || got == nil || *got != s {
		t.Fatalf("sso settings = %+v, %v", got, err)
	}
}

func TestAddKeyProfile(t *testing.T) {
	_, creds := useTempAWSFiles(t, "")
	if err := AddKeyProfile("ci", "eu-central-1", "AKIDEXAMPLE", "secret"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(context.Background(), "ci", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil || c.AccessKeyID != "AKIDEXAMPLE" || c.SecretAccessKey != "secret" || cfg.Region != "eu-central-1" {
		t.Fatalf("creds = %+v region = %q err = %v", c, cfg.Region, err)
	}
	info, _ := os.Stat(creds)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestValidateName(t *testing.T) {
	taken := []string{"dev"}
	for name, ok := range map[string]bool{"prod": true, "my.profile_1": true, "": false, "dev": false, "a b": false, "x]": false} {
		if err := ValidateName(name, taken); (err == nil) != ok {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
}
