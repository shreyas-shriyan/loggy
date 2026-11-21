package awsauth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
)

func TestAuthorizeURL(t *testing.T) {
	// Verifier and challenge from RFC 7636 appendix B.
	got := authorizeURL("https://oidc.eu-west-1.amazonaws.com", "cid", "http://127.0.0.1:1234/oauth/callback", "st", "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "oidc.eu-west-1.amazonaws.com" || u.Path != "/authorize" ||
		q.Get("code_challenge") != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" || q.Get("code_challenge_method") != "S256" ||
		q.Get("client_id") != "cid" || q.Get("state") != "st" || q.Get("response_type") != "code" ||
		q.Get("redirect_uri") != "http://127.0.0.1:1234/oauth/callback" || q.Get("scopes") != "sso:account:access" {
		t.Fatalf("url = %s", got)
	}
}

func TestOIDCEndpoint(t *testing.T) {
	ep, err := ssooidc.NewDefaultEndpointResolverV2().ResolveEndpoint(context.Background(), ssooidc.EndpointParameters{Region: aws.String("eu-west-1")})
	if err != nil || ep.URI.String() != "https://oidc.eu-west-1.amazonaws.com" {
		t.Fatalf("endpoint = %v, %v", ep.URI.String(), err)
	}
}

func TestCallbackServer(t *testing.T) {
	cb, err := startCallback("expected")
	if err != nil {
		t.Fatal(err)
	}
	defer cb.close()
	if !strings.HasPrefix(cb.redirectURI(), "http://127.0.0.1:") || !strings.HasSuffix(cb.redirectURI(), callbackPath) {
		t.Fatalf("redirect = %s", cb.redirectURI())
	}

	get := func(query string) int {
		t.Helper()
		resp, err := http.Get(cb.redirectURI() + "?" + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("state=wrong&code=evil"); code != http.StatusBadRequest {
		t.Fatalf("wrong state status = %d", code)
	}
	if code := get("state=expected&code=good"); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	code, err := cb.wait(context.Background(), time.Second)
	if err != nil || code != "good" {
		t.Fatalf("wait = %q, %v", code, err)
	}
}

func TestCallbackServerError(t *testing.T) {
	cb, err := startCallback("s")
	if err != nil {
		t.Fatal(err)
	}
	defer cb.close()
	resp, err := http.Get(cb.redirectURI() + "?state=s&error=access_denied")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := cb.wait(context.Background(), time.Second); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v", err)
	}
}
