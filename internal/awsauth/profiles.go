package awsauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
)

// SSOSessions returns the [sso-session] sections of the shared config file.
func SSOSessions() []SSOSettings {
	var out []SSOSettings
	for _, s := range readSections(ConfigFile()) {
		name, ok := strings.CutPrefix(s.name, "sso-session ")
		if !ok || s.keys["sso_start_url"] == "" {
			continue
		}
		out = append(out, SSOSettings{
			SessionName: strings.TrimSpace(name),
			StartURL:    s.keys["sso_start_url"],
			Region:      s.keys["sso_region"],
		})
	}
	return out
}

// ValidateName checks that name can be used as a new profile or sso-session name.
func ValidateName(name string, taken []string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("required")
	case strings.ContainsAny(name, " \t[]=#;"):
		return errors.New("use letters, digits, '-', '_' or '.' only")
	case slices.Contains(taken, name):
		return errors.New("already exists")
	}
	return nil
}

// SSOToken returns an access token for the sso-session s, reusing the cached
// token when it is still valid and signing in through the browser otherwise.
func SSOToken(ctx context.Context, s SSOSettings, out io.Writer) (string, error) {
	if tok, err := cachedAccessToken(ctx, s); err == nil {
		return tok, nil
	}
	if err := Login(ctx, s, out); err != nil {
		return "", err
	}
	return cachedAccessToken(ctx, s)
}

func cachedAccessToken(ctx context.Context, s SSOSettings) (string, error) {
	path, err := ssocreds.StandardCachedTokenFilepath(s.SessionName)
	if err != nil {
		return "", err
	}
	client := ssooidc.New(ssooidc.Options{Region: s.Region, Credentials: aws.AnonymousCredentials{}})
	tok, err := ssocreds.NewSSOTokenProvider(client, path).RetrieveBearerToken(ctx)
	if err != nil {
		return "", err
	}
	return tok.Value, nil
}

// Account is an AWS account the SSO user can access.
type Account struct {
	ID   string
	Name string
}

// ListAccounts returns the accounts available to token, sorted by name.
func ListAccounts(ctx context.Context, region, token string) ([]Account, error) {
	client := sso.New(sso.Options{Region: region})
	var out []Account
	p := sso.NewListAccountsPaginator(client, &sso.ListAccountsInput{AccessToken: aws.String(token)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list SSO accounts: %w", err)
		}
		for _, a := range page.AccountList {
			out = append(out, Account{ID: aws.ToString(a.AccountId), Name: aws.ToString(a.AccountName)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// ListRoles returns the role names token may assume in accountID.
func ListRoles(ctx context.Context, region, token, accountID string) ([]string, error) {
	client := sso.New(sso.Options{Region: region})
	var out []string
	p := sso.NewListAccountRolesPaginator(client, &sso.ListAccountRolesInput{
		AccessToken: aws.String(token),
		AccountId:   aws.String(accountID),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list SSO roles: %w", err)
		}
		for _, r := range page.RoleList {
			out = append(out, aws.ToString(r.RoleName))
		}
	}
	sort.Strings(out)
	return out, nil
}

// SSOProfile is a new IAM Identity Center profile.
type SSOProfile struct {
	Name      string
	Region    string
	AccountID string
	RoleName  string
	Session   SSOSettings
}

// AddSSOProfile appends p to the shared config file, adding its sso-session
// section when the config does not have one with that name yet.
func AddSSOProfile(p SSOProfile) error {
	var b strings.Builder
	if !slices.ContainsFunc(SSOSessions(), func(s SSOSettings) bool { return s.SessionName == p.Session.SessionName }) {
		fmt.Fprintf(&b, "[sso-session %s]\nsso_start_url = %s\nsso_region = %s\nsso_registration_scopes = sso:account:access\n\n",
			p.Session.SessionName, p.Session.StartURL, p.Session.Region)
	}
	fmt.Fprintf(&b, "%s\nsso_session = %s\nsso_account_id = %s\nsso_role_name = %s\nregion = %s\n",
		configHeader(p.Name), p.Session.SessionName, p.AccountID, p.RoleName, p.Region)
	return appendSection(ConfigFile(), b.String())
}

// AddKeyProfile stores an access key in the shared credentials file and the
// profile's region in the shared config file.
func AddKeyProfile(name, region, keyID, secret string) error {
	creds := fmt.Sprintf("[%s]\naws_access_key_id = %s\naws_secret_access_key = %s\n", name, keyID, secret)
	if err := appendSection(CredentialsFile(), creds); err != nil {
		return err
	}
	return appendSection(ConfigFile(), fmt.Sprintf("%s\nregion = %s\n", configHeader(name), region))
}

func configHeader(profile string) string {
	if profile == "default" {
		return "[default]"
	}
	return "[profile " + profile + "]"
}

// appendSection appends text to path as a new block separated by a blank line,
// creating the file with owner-only permissions if needed.
func appendSection(path, text string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(existing) > 0 {
		if !strings.HasSuffix(string(existing), "\n") {
			text = "\n" + text
		}
		text = "\n" + text
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
