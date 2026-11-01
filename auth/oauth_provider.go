package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthProvider: exchange + ambil email SAJA. Tidak ada account-matching
// di sini (tahap berikutnya).
type OAuthProvider interface {
	AuthURL(state, redirectURI string) string
	Exchange(code, redirectURI string) (accessToken string, err error)
	FetchEmail(accessToken string) (email string, err error)
}

var httpClientOAuth = &http.Client{Timeout: 10 * time.Second}

// ---------------- GitHub ----------------

type GithubProvider struct {
	ClientID     string
	ClientSecret string
}

func (p *GithubProvider) AuthURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", "user:email")
	v.Set("state", state)
	return "https://github.com/login/oauth/authorize?" + v.Encode()
}

func (p *GithubProvider) Exchange(code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest(http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClientOAuth.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github token exchange failed: status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("github token exchange error: %s: %s", out.Error, out.ErrorDesc)
	}
	if out.AccessToken == "" {
		return "", errors.New("github token exchange: empty access_token")
	}
	return out.AccessToken, nil
}

func (p *GithubProvider) FetchEmail(accessToken string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClientOAuth.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github fetch emails failed: status %d: %s", resp.StatusCode, string(body))
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}

	var verifiedFallback, anyFallback string
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
		if e.Verified && verifiedFallback == "" {
			verifiedFallback = e.Email
		}
		if anyFallback == "" {
			anyFallback = e.Email
		}
	}
	if verifiedFallback != "" {
		return verifiedFallback, nil
	}
	if anyFallback != "" {
		return anyFallback, nil
	}
	return "", errors.New("github: no email found on profile")
}

// ---------------- Google ----------------

type GoogleProvider struct {
	ClientID     string
	ClientSecret string
}

func (p *GoogleProvider) AuthURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("response_type", "code")
	v.Set("scope", "openid email profile")
	v.Set("state", state)
	v.Set("access_type", "online")
	return "https://accounts.google.com/o/oauth2/v2/auth?" + v.Encode()
}

func (p *GoogleProvider) Exchange(code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	resp, err := httpClientOAuth.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google token exchange failed: status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("google token exchange error: %s: %s", out.Error, out.ErrorDesc)
	}
	if out.AccessToken == "" {
		return "", errors.New("google token exchange: empty access_token")
	}
	return out.AccessToken, nil
}

func (p *GoogleProvider) FetchEmail(accessToken string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := httpClientOAuth.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google fetch userinfo failed: status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Email == "" {
		return "", errors.New("google: no email found on profile")
	}
	if !out.EmailVerified {
		return "", errors.New("google: email not verified")
	}
	return out.Email, nil
}

// NewProviders hanya mendaftarkan provider yang kredensialnya terisi.
func NewProviders(cfg *OAuthConfig) map[string]OAuthProvider {
	providers := make(map[string]OAuthProvider)
	if cfg.Enabled("github") {
		providers["github"] = &GithubProvider{ClientID: cfg.GithubClientID, ClientSecret: cfg.GithubClientSecret}
	}
	if cfg.Enabled("google") {
		providers["google"] = &GoogleProvider{ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret}
	}
	return providers
}
