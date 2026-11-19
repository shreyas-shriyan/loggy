package eks

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func TestTokenIsPresignedForCluster(t *testing.T) {
	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", ""),
	}
	ts := &tokenSource{sts: sts.NewPresignClient(sts.NewFromConfig(cfg)), cluster: "my-cluster"}
	tok, err := ts.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, tokenPrefix) {
		t.Fatalf("token %q lacks prefix", tok)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tok, tokenPrefix))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("Action") != "GetCallerIdentity" || q.Get("X-Amz-Expires") != "60" {
		t.Fatalf("unexpected query %v", q)
	}
	if !strings.Contains(q.Get("X-Amz-SignedHeaders"), clusterHeader) {
		t.Fatalf("signed headers %q missing %s", q.Get("X-Amz-SignedHeaders"), clusterHeader)
	}
	if again, _ := ts.Token(context.Background()); again != tok {
		t.Fatal("token was not cached")
	}
}
