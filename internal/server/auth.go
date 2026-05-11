package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ohmesh/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const githubOAuthAuthorizeURL = "https://github.com/login/oauth/authorize"
const githubOAuthTokenURL = "https://github.com/login/oauth/access_token"
const githubAPIBaseURL = "https://api.github.com"
const googleOAuthAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
const googleOAuthTokenURL = "https://oauth2.googleapis.com/token"
const googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

type oauthState struct {
	AppSlug     string    `json:"app_slug"`
	RedirectURL string    `json:"redirect_url"`
	Nonce       string    `json:"nonce"`
	CreatedAt   time.Time `json:"created_at"`
}

type githubTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

type githubUserResponse struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

type githubEmailResponse struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type googleTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	Scope            string `json:"scope"`
	IDToken          string `json:"id_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type googleUserResponse struct {
	Sub      string `json:"sub"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Picture  string `json:"picture"`
	Verified bool   `json:"email_verified"`
}

type oauthProfile struct {
	Provider       string
	ProviderUserID string
	Email          string
	Name           string
	AvatarURL      string
	AccessToken    string
	RefreshToken   string
}

func (s *Server) githubLogin(c *gin.Context) {
	if s.cfg.GitHubClientID == "" || s.cfg.GitHubClientSecret == "" {
		respondError(c, http.StatusServiceUnavailable, "GitHub OAuth is not configured")
		return
	}

	app, redirectURL, ok := s.oauthStartParams(c)
	if !ok {
		return
	}

	nonce, err := randomURLToken(16)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := s.signState(oauthState{
		AppSlug:     app.Slug,
		RedirectURL: redirectURL,
		Nonce:       nonce,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	values := url.Values{}
	values.Set("client_id", s.cfg.GitHubClientID)
	values.Set("scope", "read:user user:email")
	values.Set("state", state)

	c.Redirect(http.StatusFound, githubOAuthAuthorizeURL+"?"+values.Encode())
}

func (s *Server) googleLogin(c *gin.Context) {
	if s.cfg.GoogleClientID == "" || s.cfg.GoogleClientSecret == "" {
		respondError(c, http.StatusServiceUnavailable, "Google OAuth is not configured")
		return
	}

	app, redirectURL, ok := s.oauthStartParams(c)
	if !ok {
		return
	}

	nonce, err := randomURLToken(16)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := s.signState(oauthState{
		AppSlug:     app.Slug,
		RedirectURL: redirectURL,
		Nonce:       nonce,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	values := url.Values{}
	values.Set("client_id", s.cfg.GoogleClientID)
	values.Set("redirect_uri", callbackURL(c, "/auth/google/callback"))
	values.Set("response_type", "code")
	values.Set("scope", "openid email profile")
	values.Set("state", state)
	values.Set("access_type", "offline")
	values.Set("prompt", "select_account")

	c.Redirect(http.StatusFound, googleOAuthAuthorizeURL+"?"+values.Encode())
}

func (s *Server) githubCallback(c *gin.Context) {
	if githubErr := c.Query("error"); githubErr != "" {
		respondError(c, http.StatusBadRequest, "GitHub OAuth error: "+githubErr)
		return
	}

	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		respondError(c, http.StatusBadRequest, "code is required")
		return
	}

	var state oauthState
	if err := s.verifyState(c.Query("state"), &state); err != nil {
		respondError(c, http.StatusBadRequest, "invalid OAuth state")
		return
	}
	if time.Since(state.CreatedAt) > 10*time.Minute {
		respondError(c, http.StatusBadRequest, "OAuth state expired")
		return
	}

	app, err := s.loadActiveApp(state.AppSlug)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}
	if !s.redirectAllowed(app, state.RedirectURL) {
		respondError(c, http.StatusBadRequest, "redirect_url is not registered for app")
		return
	}

	token, err := s.exchangeGitHubCode(c.Request.Context(), code)
	if err != nil {
		respondError(c, http.StatusBadGateway, "GitHub token exchange failed")
		return
	}

	githubUser, err := s.fetchGitHubUser(c.Request.Context(), token.AccessToken)
	if err != nil {
		respondError(c, http.StatusBadGateway, "GitHub user fetch failed")
		return
	}

	user, err := s.upsertGitHubUser(githubUser, token)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := s.createSession(c, user.ID, app.ID); err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	c.Redirect(http.StatusFound, appendQuery(state.RedirectURL, "ohmesh_login", "success"))
}

func (s *Server) googleCallback(c *gin.Context) {
	if googleErr := c.Query("error"); googleErr != "" {
		respondError(c, http.StatusBadRequest, "Google OAuth error: "+googleErr)
		return
	}

	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		respondError(c, http.StatusBadRequest, "code is required")
		return
	}

	var state oauthState
	if err := s.verifyState(c.Query("state"), &state); err != nil {
		respondError(c, http.StatusBadRequest, "invalid OAuth state")
		return
	}
	if time.Since(state.CreatedAt) > 10*time.Minute {
		respondError(c, http.StatusBadRequest, "OAuth state expired")
		return
	}

	app, err := s.loadActiveApp(state.AppSlug)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}
	if !s.redirectAllowed(app, state.RedirectURL) {
		respondError(c, http.StatusBadRequest, "redirect_url is not registered for app")
		return
	}

	token, err := s.exchangeGoogleCode(c.Request.Context(), code, callbackURL(c, "/auth/google/callback"))
	if err != nil {
		respondError(c, http.StatusBadGateway, "Google token exchange failed")
		return
	}

	googleUser, err := s.fetchGoogleUser(c.Request.Context(), token.AccessToken)
	if err != nil {
		respondError(c, http.StatusBadGateway, "Google user fetch failed")
		return
	}

	user, err := s.upsertOAuthUser(oauthProfile{
		Provider:       models.IdentityProviderGoogle,
		ProviderUserID: googleUser.Sub,
		Email:          googleUser.Email,
		Name:           googleUser.Name,
		AvatarURL:      googleUser.Picture,
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := s.createSession(c, user.ID, app.ID); err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	c.Redirect(http.StatusFound, appendQuery(state.RedirectURL, "ohmesh_login", "success"))
}

func (s *Server) me(c *gin.Context) {
	session, user, ok := s.sessionFromRequest(c)
	if !ok {
		return
	}

	var app models.App
	if err := s.db.First(&app, session.AppID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user,
		"app": gin.H{
			"id":   app.ID,
			"slug": app.Slug,
			"name": app.Name,
		},
		"session": gin.H{
			"expires_at": session.ExpiresAt,
		},
	})
}

func (s *Server) logout(c *gin.Context) {
	token, err := c.Cookie(s.cfg.SessionCookieName)
	if err == nil && token != "" {
		s.db.Where("token_hash = ?", hashToken(token)).Delete(&models.Session{})
	}

	s.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) redirectAllowed(app models.App, redirectURL string) bool {
	if app.DefaultRedirectURL != "" && urlWithinBase(redirectURL, app.DefaultRedirectURL) {
		return true
	}

	var domains []models.AppDomain
	if err := s.db.Where("app_id = ?", app.ID).Find(&domains).Error; err != nil {
		return false
	}
	for _, domain := range domains {
		if urlWithinBase(redirectURL, domain.Domain) {
			return true
		}
	}
	return false
}

func (s *Server) oauthStartParams(c *gin.Context) (models.App, string, bool) {
	app, err := s.loadActiveApp(c.Query("app"))
	if err != nil {
		respondDBError(c, err, "app not found")
		return models.App{}, "", false
	}

	redirectURL := strings.TrimSpace(c.Query("redirect_url"))
	if redirectURL == "" {
		redirectURL = app.DefaultRedirectURL
	}
	if redirectURL == "" {
		respondError(c, http.StatusBadRequest, "redirect_url is required")
		return models.App{}, "", false
	}

	redirectURL, err = normalizeBaseURL(redirectURL)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid redirect_url")
		return models.App{}, "", false
	}
	if !s.redirectAllowed(app, redirectURL) {
		respondError(c, http.StatusBadRequest, "redirect_url is not registered for app")
		return models.App{}, "", false
	}

	return app, redirectURL, true
}

func (s *Server) exchangeGitHubCode(ctx context.Context, code string) (githubTokenResponse, error) {
	values := url.Values{}
	values.Set("client_id", s.cfg.GitHubClientID)
	values.Set("client_secret", s.cfg.GitHubClientSecret)
	values.Set("code", code)

	var token githubTokenResponse
	if err := oauthFormRequest(ctx, githubOAuthTokenURL, values, &token); err != nil {
		return githubTokenResponse{}, err
	}
	if token.Error != "" {
		return githubTokenResponse{}, fmt.Errorf("github oauth error: %s", token.Error)
	}
	if token.AccessToken == "" {
		return githubTokenResponse{}, errors.New("github oauth response did not include an access token")
	}
	return token, nil
}

func (s *Server) fetchGitHubUser(ctx context.Context, accessToken string) (githubUserResponse, error) {
	var githubUser githubUserResponse
	if err := s.oauthJSONRequest(ctx, http.MethodGet, githubAPIBaseURL+"/user", accessToken, nil, &githubUser); err != nil {
		return githubUserResponse{}, err
	}
	if githubUser.ID == 0 {
		return githubUserResponse{}, errors.New("github user response did not include an id")
	}

	if githubUser.Email == "" {
		var emails []githubEmailResponse
		if err := s.oauthJSONRequest(ctx, http.MethodGet, githubAPIBaseURL+"/user/emails", accessToken, nil, &emails); err == nil {
			githubUser.Email = selectGitHubEmail(emails)
		}
	}

	return githubUser, nil
}

func (s *Server) upsertGitHubUser(githubUser githubUserResponse, token githubTokenResponse) (models.User, error) {
	return s.upsertOAuthUser(oauthProfile{
		Provider:       models.IdentityProviderGitHub,
		ProviderUserID: strconv.FormatInt(githubUser.ID, 10),
		Email:          githubUser.Email,
		Name:           fallbackString(githubUser.Name, githubUser.Login),
		AvatarURL:      githubUser.AvatarURL,
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
	})
}

func (s *Server) exchangeGoogleCode(ctx context.Context, code, redirectURI string) (googleTokenResponse, error) {
	values := url.Values{}
	values.Set("client_id", s.cfg.GoogleClientID)
	values.Set("client_secret", s.cfg.GoogleClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	values.Set("redirect_uri", redirectURI)

	var token googleTokenResponse
	if err := oauthFormRequest(ctx, googleOAuthTokenURL, values, &token); err != nil {
		return googleTokenResponse{}, err
	}
	if token.Error != "" {
		return googleTokenResponse{}, fmt.Errorf("google oauth error: %s", token.Error)
	}
	if token.AccessToken == "" {
		return googleTokenResponse{}, errors.New("google oauth response did not include an access token")
	}
	return token, nil
}

func (s *Server) fetchGoogleUser(ctx context.Context, accessToken string) (googleUserResponse, error) {
	var googleUser googleUserResponse
	if err := s.oauthJSONRequest(ctx, http.MethodGet, googleUserInfoURL, accessToken, nil, &googleUser); err != nil {
		return googleUserResponse{}, err
	}
	if googleUser.Sub == "" {
		return googleUserResponse{}, errors.New("google user response did not include a subject")
	}
	return googleUser, nil
}

func (s *Server) upsertOAuthUser(profile oauthProfile) (models.User, error) {
	var user models.User
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var identity models.Identity
		err := tx.Where("provider = ? AND provider_user_id = ?", profile.Provider, profile.ProviderUserID).First(&identity).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			user = models.User{
				Email:     profile.Email,
				Name:      profile.Name,
				AvatarURL: profile.AvatarURL,
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}

			identity = models.Identity{
				UserID:         user.ID,
				Provider:       profile.Provider,
				ProviderUserID: profile.ProviderUserID,
				AccessToken:    profile.AccessToken,
				RefreshToken:   profile.RefreshToken,
			}
			return tx.Create(&identity).Error
		}
		if err != nil {
			return err
		}

		if err := tx.First(&user, identity.UserID).Error; err != nil {
			return err
		}

		user.Email = profile.Email
		user.Name = profile.Name
		user.AvatarURL = profile.AvatarURL
		if err := tx.Save(&user).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"access_token": profile.AccessToken,
		}
		if profile.RefreshToken != "" {
			updates["refresh_token"] = profile.RefreshToken
		}
		return tx.Model(&identity).Updates(updates).Error
	})

	return user, err
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func selectGitHubEmail(emails []githubEmailResponse) string {
	for _, email := range emails {
		if email.Primary && email.Verified {
			return email.Email
		}
	}
	for _, email := range emails {
		if email.Primary {
			return email.Email
		}
	}
	for _, email := range emails {
		if email.Verified {
			return email.Email
		}
	}
	if len(emails) > 0 {
		return emails[0].Email
	}
	return ""
}

func (s *Server) oauthJSONRequest(ctx context.Context, method, rawURL, accessToken string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ohmesh")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("oauth request failed: status=%d body=%s", resp.StatusCode, string(responseBody))
	}

	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func oauthFormRequest(ctx context.Context, rawURL string, values url.Values, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "ohmesh")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("oauth form request failed: status=%d body=%s", resp.StatusCode, string(responseBody))
	}

	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
