package cli

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/shreyas-shriyan/loggy/internal/awsauth"
	"github.com/shreyas-shriyan/loggy/internal/tui"
)

const keyNewProfile = "__new_profile__"

// newProfile creates an AWS profile and returns its name and default region.
func (a *App) newProfile(ctx context.Context, defRegion string) (string, string, error) {
	kind, err := tui.Select("How does the new profile sign in?", []tui.Opt{
		{Key: "sso", Label: "IAM Identity Center (SSO)  " + tui.Faint("— browser sign-in, like `aws configure sso`")},
		{Key: "keys", Label: "Access key  " + tui.Faint("— access key ID and secret")},
	})
	if err != nil {
		return "", "", err
	}
	if kind == "keys" {
		return a.newKeyProfile(defRegion)
	}
	return a.newSSOProfile(ctx, defRegion)
}

func (a *App) newSSOProfile(ctx context.Context, defRegion string) (string, string, error) {
	sess, err := a.pickSSOSession()
	if err != nil {
		return "", "", err
	}
	a.printf("Signing in to %s...\n", sess.StartURL)
	token, err := awsauth.SSOToken(ctx, sess, a.out)
	if err != nil {
		return "", "", err
	}

	accounts, err := awsauth.ListAccounts(ctx, sess.Region, token)
	if err != nil {
		return "", "", err
	}
	if len(accounts) == 0 {
		return "", "", fmt.Errorf("no AWS accounts are assigned to you in %s", sess.StartURL)
	}
	opts := make([]tui.Opt, len(accounts))
	names := map[string]string{}
	for i, acc := range accounts {
		opts[i] = tui.Opt{Key: acc.ID, Label: acc.Name + "  " + tui.Faint(acc.ID)}
		names[acc.ID] = acc.Name
	}
	accountID, err := tui.Select("AWS account", opts)
	if err != nil {
		return "", "", err
	}

	roles, err := awsauth.ListRoles(ctx, sess.Region, token, accountID)
	if err != nil {
		return "", "", err
	}
	var role string
	switch len(roles) {
	case 0:
		return "", "", fmt.Errorf("you have no roles in account %s", accountID)
	case 1:
		role = roles[0]
		a.printf("Using the only available role, %s.\n", role)
	default:
		if role, err = tui.SelectStrings("Role", roles, nil); err != nil {
			return "", "", err
		}
	}

	taken := awsauth.ListProfiles()
	name, err := a.profileName(suggestProfileName(names[accountID], role, taken), taken)
	if err != nil {
		return "", "", err
	}
	region, err := tui.Input("Default AWS region for the profile", defRegion, defRegion, nonEmpty)
	if err != nil {
		return "", "", err
	}
	p := awsauth.SSOProfile{Name: name, Region: region, AccountID: accountID, RoleName: role, Session: sess}
	if err := awsauth.AddSSOProfile(p); err != nil {
		return "", "", err
	}
	a.printf("Added profile %s to %s\n", name, awsauth.ConfigFile())
	return name, region, nil
}

// pickSSOSession chooses an existing sso-session or describes a new one.
func (a *App) pickSSOSession() (awsauth.SSOSettings, error) {
	sessions := awsauth.SSOSessions()
	if len(sessions) > 0 {
		opts := make([]tui.Opt, 0, len(sessions)+1)
		for _, s := range sessions {
			opts = append(opts, tui.Opt{Key: s.SessionName, Label: s.SessionName + "  " + tui.Faint(s.StartURL)})
		}
		opts = append(opts, tui.Opt{Key: keyCustom, Label: "New SSO start URL..."})
		key, err := tui.Select("SSO session", opts)
		if err != nil {
			return awsauth.SSOSettings{}, err
		}
		for _, s := range sessions {
			if s.SessionName == key {
				return s, nil
			}
		}
	}

	start, err := tui.Input("SSO start URL", "", "https://my-org.awsapps.com/start", validStartURL)
	if err != nil {
		return awsauth.SSOSettings{}, err
	}
	region, err := tui.Input("SSO region", "us-east-1", "us-east-1", nonEmpty)
	if err != nil {
		return awsauth.SSOSettings{}, err
	}
	taken := make([]string, len(sessions))
	for i, s := range sessions {
		taken[i] = s.SessionName
	}
	name, err := tui.Input("Name for this SSO session", sessionNameFromURL(start), "my-org", func(s string) error {
		return awsauth.ValidateName(s, taken)
	})
	if err != nil {
		return awsauth.SSOSettings{}, err
	}
	return awsauth.SSOSettings{SessionName: name, StartURL: start, Region: region}, nil
}

func (a *App) newKeyProfile(defRegion string) (string, string, error) {
	taken := awsauth.ListProfiles()
	name, err := a.profileName("", taken)
	if err != nil {
		return "", "", err
	}
	keyID, err := tui.Input("Access key ID", "", "AKIA...", nonEmpty)
	if err != nil {
		return "", "", err
	}
	secret, err := tui.Password("Secret access key", nonEmpty)
	if err != nil {
		return "", "", err
	}
	region, err := tui.Input("Default AWS region for the profile", defRegion, defRegion, nonEmpty)
	if err != nil {
		return "", "", err
	}
	if err := awsauth.AddKeyProfile(name, region, keyID, secret); err != nil {
		return "", "", err
	}
	a.printf("Added profile %s to %s\n", name, awsauth.CredentialsFile())
	return name, region, nil
}

func (a *App) profileName(suggestion string, taken []string) (string, error) {
	return tui.Input("Profile name", suggestion, "my-profile", func(s string) error {
		return awsauth.ValidateName(s, taken)
	})
}

var nonSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// suggestProfileName proposes the account name, adding the role when that is taken.
func suggestProfileName(account, role string, taken []string) string {
	base := slug(account)
	if base == "" {
		return ""
	}
	for _, name := range []string{base, base + "-" + slug(role)} {
		if awsauth.ValidateName(name, taken) == nil {
			return name
		}
	}
	return ""
}

func validStartURL(s string) error {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("enter an https:// URL")
	}
	return nil
}

// sessionNameFromURL suggests "my-org" for https://my-org.awsapps.com/start.
func sessionNameFromURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	host, _, _ := strings.Cut(u.Hostname(), ".")
	return slug(host)
}
