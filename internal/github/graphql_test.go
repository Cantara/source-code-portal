package github

import (
	"net/url"
	"testing"
)

func TestGraphQLEndpoint(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.github.com/":         "https://api.github.com/graphql",
		"https://ghe.example.org/api/v3/": "https://ghe.example.org/api/graphql",
		"http://127.0.0.1:5000/":          "http://127.0.0.1:5000/graphql",
	} {
		u, _ := url.Parse(base)
		if got := graphQLEndpoint(u); got != want {
			t.Errorf("graphQLEndpoint(%s) = %s, want %s", base, got, want)
		}
	}
}
