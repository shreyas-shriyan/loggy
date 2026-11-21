package cli

import "testing"

func TestSuggestProfileName(t *testing.T) {
	cases := []struct {
		account, role string
		taken         []string
		want          string
	}{
		{"Shop Dev", "ReadOnly", nil, "shop-dev"},
		{"Shop Dev", "ReadOnly", []string{"shop-dev"}, "shop-dev-readonly"},
		{"Shop Dev", "ReadOnly", []string{"shop-dev", "shop-dev-readonly"}, ""},
		{"", "ReadOnly", nil, ""},
	}
	for _, c := range cases {
		if got := suggestProfileName(c.account, c.role, c.taken); got != c.want {
			t.Errorf("suggestProfileName(%q, %q, %v) = %q, want %q", c.account, c.role, c.taken, got, c.want)
		}
	}
}

func TestSessionNameFromURL(t *testing.T) {
	if got := sessionNameFromURL("https://My-Org.awsapps.com/start#/"); got != "my-org" {
		t.Fatalf("got %q", got)
	}
	if validStartURL("http://x.awsapps.com/start") == nil || validStartURL("https://x.awsapps.com/start") != nil {
		t.Fatal("validStartURL")
	}
}
