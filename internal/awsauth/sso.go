package awsauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
)

// SSOSettings identifies an IAM Identity Center sign-in.
type SSOSettings struct {
	// SessionName is set for [sso-session] profiles and empty for legacy SSO profiles.
	SessionName string
	StartURL    string
	Region      string
}

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// deviceLogin runs the OIDC device authorization flow and writes the token to
// ~/.aws/sso/cache in the format the AWS CLI and SDKs read.
func deviceLogin(ctx context.Context, s SSOSettings, out io.Writer) error {
	client := ssooidc.New(ssooidc.Options{Region: s.Region, Credentials: aws.AnonymousCredentials{}})

	reg := &ssooidc.RegisterClientInput{ClientName: aws.String("loggy"), ClientType: aws.String("public")}
	if s.SessionName != "" {
		reg.Scopes = []string{"sso:account:access"}
	}
	registered, err := client.RegisterClient(ctx, reg)
	if err != nil {
		return fmt.Errorf("register SSO client: %w", err)
	}

	auth, err := client.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     registered.ClientId,
		ClientSecret: registered.ClientSecret,
		StartUrl:     aws.String(s.StartURL),
	})
	if err != nil {
		return fmt.Errorf("start SSO device authorization: %w", err)
	}

	url := aws.ToString(auth.VerificationUriComplete)
	fmt.Fprintf(out, "\nOpen this URL to sign in (code %s):\n  %s\n\n", aws.ToString(auth.UserCode), url)
	if err := openBrowser(url); err != nil {
		fmt.Fprintln(out, "Could not open a browser automatically; open the URL above manually.")
	}
	fmt.Fprintln(out, "Waiting for browser sign-in...")

	interval := time.Duration(auth.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return errors.New("SSO sign-in timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		tok, err := client.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     registered.ClientId,
			ClientSecret: registered.ClientSecret,
			DeviceCode:   auth.DeviceCode,
			GrantType:    aws.String(deviceGrantType),
		})
		var pending *types.AuthorizationPendingException
		var slow *types.SlowDownException
		switch {
		case errors.As(err, &pending):
			continue
		case errors.As(err, &slow):
			interval += 5 * time.Second
			continue
		case err != nil:
			return fmt.Errorf("SSO sign-in failed: %w", err)
		}
		if err := writeCache(s, registered, tok); err != nil {
			return fmt.Errorf("save SSO token: %w", err)
		}
		fmt.Fprintln(out, "Signed in.")
		return nil
	}
}

type cachedToken struct {
	StartURL              string `json:"startUrl"`
	Region                string `json:"region"`
	AccessToken           string `json:"accessToken"`
	ExpiresAt             string `json:"expiresAt"`
	ClientID              string `json:"clientId,omitempty"`
	ClientSecret          string `json:"clientSecret,omitempty"`
	RegistrationExpiresAt string `json:"registrationExpiresAt,omitempty"`
	RefreshToken          string `json:"refreshToken,omitempty"`
}

func writeCache(s SSOSettings, reg *ssooidc.RegisterClientOutput, tok *ssooidc.CreateTokenOutput) error {
	key := s.StartURL
	if s.SessionName != "" {
		key = s.SessionName
	}
	path, err := ssocreds.StandardCachedTokenFilepath(key)
	if err != nil {
		return err
	}
	ct := cachedToken{
		StartURL:    s.StartURL,
		Region:      s.Region,
		AccessToken: aws.ToString(tok.AccessToken),
		ExpiresAt:   time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UTC().Format(time.RFC3339),
	}
	if s.SessionName != "" {
		ct.ClientID = aws.ToString(reg.ClientId)
		ct.ClientSecret = aws.ToString(reg.ClientSecret)
		ct.RefreshToken = aws.ToString(tok.RefreshToken)
		if reg.ClientSecretExpiresAt > 0 {
			ct.RegistrationExpiresAt = time.Unix(reg.ClientSecretExpiresAt, 0).UTC().Format(time.RFC3339)
		}
	}
	data, err := json.Marshal(ct)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func openBrowser(url string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"open", url}}
	case "windows":
		candidates = [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		candidates = [][]string{{"wslview", url}, {"xdg-open", url}}
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if err := exec.Command(c[0], c[1:]...).Start(); err == nil {
			return nil
		}
	}
	return errors.New("no browser opener found")
}
