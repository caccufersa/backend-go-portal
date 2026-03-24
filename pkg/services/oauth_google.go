package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// googleProfile holds the fields we care about from Google's userinfo endpoint.
type googleProfile struct {
	Sub     string `json:"sub"` // stable Google user ID
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"` // profile photo URL
}

// callGoogleUserinfo uses an OAuth2 access token to fetch the user profile
// from Google's /userinfo endpoint.  This avoids verifying RS256 id_tokens
// and dealing with Google's rotating JWKS keys.
func callGoogleUserinfo(accessToken string) (*googleProfile, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha ao contactar Google: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google userinfo: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var profile googleProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, err
	}

	if profile.Sub == "" || profile.Email == "" {
		return nil, fmt.Errorf("perfil Google incompleto")
	}

	// Sanitize name for username seed
	profile.Name = strings.ReplaceAll(profile.Name, " ", "_")
	return &profile, nil
}
