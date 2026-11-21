package awsauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
)

const (
	callbackPath = "/oauth/callback"
	authTimeout  = 10 * time.Minute
	defaultScope = "sso:account:access"
)

// errNoAuthCode means the authorization code flow cannot run here and the
// device code flow should be used instead.
var errNoAuthCode = errors.New("authorization code flow unavailable")

// Login signs in to s and writes the token to ~/.aws/sso/cache. Like the AWS
// CLI, sso-session profiles use the authorization code flow with PKCE, and
// everything else, or a machine where that flow cannot run, uses device codes.
// LOGGY_SSO_DEVICE_CODE=1 forces device codes.
func Login(ctx context.Context, s SSOSettings, out io.Writer) error {
	if s.SessionName != "" && os.Getenv("LOGGY_SSO_DEVICE_CODE") != "1" {
		err := authCodeLogin(ctx, s, out)
		if !errors.Is(err, errNoAuthCode) {
			return err
		}
		fmt.Fprintf(out, "%v; signing in with a device code instead.\n", err)
	}
	return deviceLogin(ctx, s, out)
}

func authCodeLogin(ctx context.Context, s SSOSettings, out io.Writer) error {
	state, err := randomString(16)
	if err != nil {
		return err
	}
	cb, err := startCallback(state)
	if err != nil {
		return fmt.Errorf("%w: cannot listen for the browser redirect (%v)", errNoAuthCode, err)
	}
	defer cb.close()

	client := ssooidc.New(ssooidc.Options{Region: s.Region, Credentials: aws.AnonymousCredentials{}})
	registered, err := client.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName:   aws.String("loggy"),
		ClientType:   aws.String("public"),
		GrantTypes:   []string{"authorization_code", "refresh_token"},
		RedirectUris: []string{"http://127.0.0.1" + callbackPath},
		IssuerUrl:    aws.String(s.StartURL),
		Scopes:       []string{defaultScope},
	})
	if err != nil {
		return fmt.Errorf("register SSO client: %w", err)
	}

	verifier, err := randomString(32)
	if err != nil {
		return err
	}
	endpoint, err := ssooidc.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, ssooidc.EndpointParameters{Region: aws.String(s.Region)})
	if err != nil {
		return fmt.Errorf("resolve SSO endpoint: %w", err)
	}
	authURL := authorizeURL(endpoint.URI.String(), aws.ToString(registered.ClientId), cb.redirectURI(), state, verifier)

	if err := openBrowser(authURL); err != nil {
		return fmt.Errorf("%w: no browser could be opened", errNoAuthCode)
	}
	fmt.Fprintf(out, "\nOpened your browser to sign in. If it did not open, visit:\n  %s\n\nWaiting for browser sign-in...\n", authURL)

	code, err := cb.wait(ctx, authTimeout)
	if err != nil {
		return err
	}
	tok, err := client.CreateToken(ctx, &ssooidc.CreateTokenInput{
		ClientId:     registered.ClientId,
		ClientSecret: registered.ClientSecret,
		GrantType:    aws.String("authorization_code"),
		Code:         aws.String(code),
		CodeVerifier: aws.String(verifier),
		RedirectUri:  aws.String(cb.redirectURI()),
	})
	if err != nil {
		return fmt.Errorf("SSO sign-in failed: %w", err)
	}
	if err := writeCache(s, registered, tok); err != nil {
		return fmt.Errorf("save SSO token: %w", err)
	}
	fmt.Fprintln(out, "Signed in.")
	return nil
}

// authorizeURL builds the IAM Identity Center authorize URL for a PKCE request.
func authorizeURL(endpoint, clientID, redirectURI, state, verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge_method": {"S256"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"scopes":                {defaultScope},
	}
	return endpoint + "/authorize?" + q.Encode()
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type callbackResult struct {
	code string
	err  error
}

// callbackServer receives the browser redirect on 127.0.0.1. Any local
// process or web page can reach it, so only requests carrying state count.
type callbackServer struct {
	ln     net.Listener
	srv    *http.Server
	state  string
	result chan callbackResult
}

func startCallback(state string) (*callbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := &callbackServer{ln: ln, state: state, result: make(chan callbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+callbackPath, c.handle)
	c.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go c.srv.Serve(ln)
	return c, nil
}

func (c *callbackServer) redirectURI() string {
	return fmt.Sprintf("http://%s%s", c.ln.Addr(), callbackPath)
}

func (c *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(c.state)) != 1 {
		http.Error(w, "This sign-in request was not started by loggy.", http.StatusBadRequest)
		return
	}
	var res callbackResult
	switch {
	case q.Get("error") != "":
		res.err = fmt.Errorf("SSO sign-in failed: %s %s", q.Get("error"), q.Get("error_description"))
		http.Error(w, "Sign-in failed. Return to loggy for details.", http.StatusOK)
	case q.Get("code") != "":
		res.code = q.Get("code")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>loggy</title><p>Signed in. You can close this tab and return to loggy.</p>")
	default:
		http.Error(w, "The request has neither an authorization code nor an error.", http.StatusBadRequest)
		return
	}
	select {
	case c.result <- res:
	default:
	}
}

func (c *callbackServer) wait(ctx context.Context, timeout time.Duration) (string, error) {
	select {
	case res := <-c.result:
		return res.code, res.err
	case <-time.After(timeout):
		return "", errors.New("SSO sign-in timed out")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *callbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.srv.Shutdown(ctx)
}
