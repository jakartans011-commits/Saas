package auth

import "os"

// OAuthConfig menampung kredensial OAuth per provider, dibaca dari env var.
// Tidak ada fallback untuk secret — kalau kosong, provider dianggap
// belum dikonfigurasi (lihat Enabled).
type OAuthConfig struct {
	GithubClientID     string
	GithubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string
}

func LoadOAuthConfigFromEnv() *OAuthConfig {
	return &OAuthConfig{
		GithubClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GithubClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
	}
}

func (c *OAuthConfig) Enabled(provider string) bool {
	switch provider {
	case "github":
		return c.GithubClientID != "" && c.GithubClientSecret != ""
	case "google":
		return c.GoogleClientID != "" && c.GoogleClientSecret != ""
	default:
		return false
	}
}
