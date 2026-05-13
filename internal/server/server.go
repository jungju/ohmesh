package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ohmesh/internal/config"
	"ohmesh/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Server struct {
	db  *gorm.DB
	cfg config.Config
}

type authedContext struct {
	App     models.App
	User    models.User
	Session models.Session
}

type adminContext struct {
	User    models.User
	Session models.Session
}

const adminSessionAppID uint = 0

func New(db *gorm.DB, cfg config.Config) *gin.Engine {
	s := &Server{db: db, cfg: cfg}

	router := gin.New()
	router.SetHTMLTemplate(mustTemplates())
	router.Use(gin.Logger(), gin.Recovery(), s.corsMiddleware())

	router.GET("/", s.homePage)
	router.GET("/login", s.loginPage)
	router.GET("/dashboard", s.requireWebAdmin(), s.dashboardPage)
	router.POST("/logout", s.webLogout)
	router.GET("/healthz", s.health)

	auth := router.Group("/auth")
	auth.GET("/github/login", s.githubLogin)
	auth.GET("/github/callback", s.githubCallback)
	auth.GET("/google/login", s.googleLogin)
	auth.GET("/google/callback", s.googleCallback)
	auth.GET("/me", s.me)
	auth.POST("/logout", s.logout)

	api := router.Group("/api")
	api.POST("/apps", s.createApp)
	api.GET("/apps", s.listApps)
	api.GET("/apps/:slug", s.getApp)
	api.PATCH("/apps/:slug", s.updateApp)
	api.DELETE("/apps/:slug", s.deleteApp)
	api.POST("/apps/:slug/domains", s.createAppDomain)
	api.GET("/apps/:slug/domains", s.listAppDomains)
	api.DELETE("/apps/:slug/domains/:id", s.deleteAppDomain)

	api.GET("/apps/:slug/records", s.listRecords)
	api.POST("/apps/:slug/records", s.createRecord)
	api.GET("/apps/:slug/records/:id", s.getRecord)
	api.PATCH("/apps/:slug/records/:id", s.updateRecord)
	api.DELETE("/apps/:slug/records/:id", s.deleteRecord)

	admin := router.Group("/admin")
	admin.Use(s.requireWebAdmin())
	admin.GET("/apps", s.webListApps)
	admin.POST("/apps", s.webCreateApp)
	admin.GET("/apps/:slug", s.webAppDetail)
	admin.POST("/apps/:slug", s.webUpdateApp)
	admin.POST("/apps/:slug/delete", s.webDeleteApp)
	admin.POST("/apps/:slug/domains", s.webCreateAppDomain)
	admin.POST("/apps/:slug/domains/:id/delete", s.webDeleteAppDomain)
	admin.GET("/apps/:slug/users", s.webAppUsers)
	admin.POST("/apps/:slug/users/:id/sessions/delete", s.webExpireAppUserSessions)
	admin.GET("/apps/:slug/db", s.webAppDB)
	admin.POST("/apps/:slug/db/records", s.webCreateAppRecord)
	admin.POST("/apps/:slug/db/records/:id", s.webUpdateAppRecord)
	admin.POST("/apps/:slug/db/records/:id/delete", s.webDeleteAppRecord)

	return router
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && s.originAllowed(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.cfg.AllowedOrigins {
		if origin == strings.TrimRight(allowed, "/") {
			return true
		}
	}

	var domains []models.AppDomain
	if err := s.db.Find(&domains).Error; err != nil {
		return false
	}

	for _, domain := range domains {
		if originFromURL(domain.Domain) == origin {
			return true
		}
	}
	return false
}

func (s *Server) loadActiveApp(slug string) (models.App, error) {
	var app models.App
	err := s.db.Where("slug = ? AND status = ?", slug, models.AppStatusActive).First(&app).Error
	return app, err
}

func (s *Server) requireAppSession(c *gin.Context) (authedContext, bool) {
	app, err := s.loadActiveApp(c.Param("slug"))
	if err != nil {
		respondDBError(c, err, "app not found")
		return authedContext{}, false
	}

	session, user, ok := s.sessionFromRequest(c)
	if !ok {
		return authedContext{}, false
	}

	if session.AppID != app.ID {
		respondError(c, http.StatusForbidden, "session is not valid for this app")
		return authedContext{}, false
	}

	return authedContext{App: app, User: user, Session: session}, true
}

func (s *Server) sessionFromRequest(c *gin.Context) (models.Session, models.User, bool) {
	session, user, ok := s.sessionFromCookie(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "login required")
		return models.Session{}, models.User{}, false
	}

	return session, user, true
}

func (s *Server) sessionFromCookie(c *gin.Context) (models.Session, models.User, bool) {
	token, err := c.Cookie(s.cfg.SessionCookieName)
	if err != nil || token == "" {
		return models.Session{}, models.User{}, false
	}

	var session models.Session
	err = s.db.Preload("User").Where("token_hash = ? AND expires_at > ?", hashToken(token), time.Now().UTC()).First(&session).Error
	if err != nil {
		return models.Session{}, models.User{}, false
	}

	return session, session.User, true
}

func (s *Server) requireAdminSession(c *gin.Context) (adminContext, bool) {
	admin, ok := s.adminSessionFromCookie(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "ohmesh login required")
		return adminContext{}, false
	}

	return admin, true
}

func (s *Server) requireWebAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		admin, ok := s.adminSessionFromCookie(c)
		if !ok {
			redirectToLogin(c)
			c.Abort()
			return
		}

		c.Set("adminUser", admin.User)
		c.Set("adminSession", admin.Session)
		c.Next()
	}
}

func (s *Server) adminSessionFromCookie(c *gin.Context) (adminContext, bool) {
	session, user, ok := s.sessionFromCookie(c)
	if !ok || session.AppID != adminSessionAppID {
		return adminContext{}, false
	}
	return adminContext{User: user, Session: session}, true
}

func (s *Server) createSession(c *gin.Context, userID, appID uint) error {
	token, err := randomURLToken(32)
	if err != nil {
		return err
	}

	expiresAt := time.Now().UTC().Add(s.cfg.SessionTTL)
	session := models.Session{
		UserID:    userID,
		AppID:     appID,
		TokenHash: hashToken(token),
		ExpiresAt: expiresAt,
	}
	if err := s.db.Create(&session).Error; err != nil {
		return err
	}

	sameSite := http.SameSiteLaxMode
	if s.cfg.CookieSecure {
		sameSite = http.SameSiteNoneMode
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: sameSite,
	})

	return nil
}

func (s *Server) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func randomURLToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) signState(payload any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(encodedBody))
	signature := mac.Sum(nil)

	return encodedBody + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *Server) verifyState(state string, target any) error {
	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return errors.New("invalid state")
	}

	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)

	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	if !hmac.Equal(actual, expected) {
		return errors.New("invalid state signature")
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func respondError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

func respondDBError(c *gin.Context, err error, notFoundMessage string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, notFoundMessage)
		return
	}
	respondError(c, http.StatusInternalServerError, "internal server error")
}

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	raw := c.Param(name)
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		respondError(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return uint(value), true
}

func normalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("URL must use http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("URL must include a host")
	}
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func originFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func urlWithinBase(rawURL, rawBase string) bool {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(rawBase))
	if err != nil {
		return false
	}
	if target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) {
		return false
	}

	basePath := strings.TrimRight(base.Path, "/")
	targetPath := strings.TrimRight(target.Path, "/")
	if basePath == "" {
		return true
	}
	return targetPath == basePath || strings.HasPrefix(targetPath, basePath+"/")
}

func appendQuery(rawURL, key, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func redirectToLogin(c *gin.Context) {
	values := url.Values{}
	values.Set("next", safeAdminPath(c.Request.URL.RequestURI()))
	c.Redirect(http.StatusSeeOther, "/login?"+values.Encode())
}

func safeAdminPath(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "/dashboard"
	}

	path := parsed.Path
	if path == "" {
		parsed.Path = "/dashboard"
		path = parsed.Path
	}
	if path != "/dashboard" && path != "/admin" && !strings.HasPrefix(path, "/admin/") {
		return "/dashboard"
	}

	parsed.Scheme = ""
	parsed.Host = ""
	parsed.User = nil
	parsed.Fragment = ""
	return parsed.RequestURI()
}

func adminNavActive(c *gin.Context) bool {
	if c.Request.URL.Path == "/admin" || strings.HasPrefix(c.Request.URL.Path, "/admin/") {
		return true
	}
	if c.Request.URL.Path != "/login" {
		return false
	}

	next := safeAdminPath(c.Query("next"))
	return next == "/admin" || strings.HasPrefix(next, "/admin/")
}

func absoluteAdminURL(c *gin.Context, rawPath string) string {
	return callbackURL(c, safeAdminPath(rawPath))
}

func adminRedirectAllowed(c *gin.Context, raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}

	current, err := url.Parse(callbackURL(c, "/"))
	if err != nil {
		return false
	}
	if parsed.Scheme != current.Scheme || !strings.EqualFold(parsed.Host, current.Host) {
		return false
	}

	path := parsed.Path
	if path == "" {
		path = "/"
	}
	return path == "/dashboard" || path == "/admin" || strings.HasPrefix(path, "/admin/")
}

func stripURLFragment(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Fragment = ""
	return parsed.String()
}

func callbackURL(c *gin.Context, path string) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}

	return scheme + "://" + host + path
}
