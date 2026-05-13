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

var (
	errAppUserLimitReached   = errors.New("app user limit reached")
	errAppRecordLimitReached = errors.New("app record limit reached")
)

func New(db *gorm.DB, cfg config.Config) *gin.Engine {
	s := &Server{db: db, cfg: cfg}

	router := gin.New()
	router.SetHTMLTemplate(mustTemplates())
	router.Use(gin.Logger(), gin.Recovery(), s.corsMiddleware())

	router.GET("/", s.homePage)
	router.GET("/login", s.loginPage)
	router.GET("/logout", s.logoutPage)
	router.GET("/llms.txt", s.llmsText)
	router.GET("/openapi.json", s.openapiJSON)
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

	session, user, ok := s.appSessionFromCookie(c, app)
	if !ok {
		respondError(c, http.StatusUnauthorized, "login required")
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
	if appSlug := strings.TrimSpace(c.Query("app")); appSlug != "" {
		if app, err := s.loadActiveApp(appSlug); err == nil {
			if session, user, ok := s.appSessionFromCookie(c, app); ok {
				return session, user, true
			}
		}
	}

	if session, user, ok := s.anyAppSessionFromCookie(c); ok {
		return session, user, true
	}
	if session, user, ok := s.adminSessionFromCookieValue(c); ok {
		return session, user, true
	}
	return s.sessionFromCookieName(c, s.cfg.SessionCookieName)
}

func (s *Server) sessionFromCookieName(c *gin.Context, name string) (models.Session, models.User, bool) {
	token, err := c.Cookie(name)
	if err != nil || token == "" {
		return models.Session{}, models.User{}, false
	}
	return s.sessionFromToken(token)
}

func (s *Server) sessionFromToken(token string) (models.Session, models.User, bool) {
	var session models.Session
	if err := s.db.Preload("User").Where("token_hash = ? AND expires_at > ?", hashToken(token), time.Now().UTC()).First(&session).Error; err != nil {
		return models.Session{}, models.User{}, false
	}
	return session, session.User, true
}

func (s *Server) appSessionFromCookie(c *gin.Context, app models.App) (models.Session, models.User, bool) {
	if session, user, ok := s.sessionFromCookieName(c, s.appSessionCookieName(app.ID)); ok {
		if session.AppID == app.ID {
			return session, user, true
		}
	}

	session, user, ok := s.sessionFromCookieName(c, s.cfg.SessionCookieName)
	if ok && session.AppID == app.ID {
		return session, user, true
	}
	return models.Session{}, models.User{}, false
}

func (s *Server) anyAppSessionFromCookie(c *gin.Context) (models.Session, models.User, bool) {
	prefix := s.cfg.SessionCookieName + "_app_"
	for _, cookie := range c.Request.Cookies() {
		if !strings.HasPrefix(cookie.Name, prefix) || cookie.Value == "" {
			continue
		}
		session, user, ok := s.sessionFromToken(cookie.Value)
		if ok && session.AppID != adminSessionAppID {
			return session, user, true
		}
	}
	return models.Session{}, models.User{}, false
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
	session, user, ok := s.adminSessionFromCookieValue(c)
	if !ok {
		return adminContext{}, false
	}
	return adminContext{User: user, Session: session}, true
}

func (s *Server) adminSessionFromCookieValue(c *gin.Context) (models.Session, models.User, bool) {
	session, user, ok := s.sessionFromCookieName(c, s.adminSessionCookieName())
	if ok && session.AppID == adminSessionAppID {
		return session, user, true
	}

	session, user, ok = s.sessionFromCookieName(c, s.cfg.SessionCookieName)
	if ok && session.AppID == adminSessionAppID {
		return session, user, true
	}
	return models.Session{}, models.User{}, false
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
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if appID != adminSessionAppID {
			if err := ensureAppUserLimit(tx, userID, appID); err != nil {
				return err
			}
		}
		return tx.Create(&session).Error
	}); err != nil {
		return err
	}

	sameSite := http.SameSiteLaxMode
	if s.cfg.CookieSecure {
		sameSite = http.SameSiteNoneMode
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     s.sessionCookieName(appID),
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

func ensureAppUserLimit(tx *gorm.DB, userID, appID uint) error {
	var app models.App
	if err := tx.First(&app, appID).Error; err != nil {
		return err
	}

	var existing int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM (
			SELECT user_id FROM sessions WHERE app_id = ? AND user_id = ?
			UNION
			SELECT user_id FROM app_records WHERE app_id = ? AND user_id = ?
		) AS existing_app_user
	`, appID, userID, appID, userID).Scan(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	var count int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM (
			SELECT user_id FROM sessions WHERE app_id = ?
			UNION
			SELECT user_id FROM app_records WHERE app_id = ?
		) AS app_users
	`, appID, appID).Scan(&count).Error; err != nil {
		return err
	}
	if count >= int64(effectiveAppUserLimit(app)) {
		return errAppUserLimitReached
	}
	return nil
}

func ensureAppRecordLimit(tx *gorm.DB, app models.App) error {
	var count int64
	if err := tx.Model(&models.AppRecord{}).Where("app_id = ?", app.ID).Count(&count).Error; err != nil {
		return err
	}
	if count >= int64(effectiveAppRecordLimit(app)) {
		return errAppRecordLimitReached
	}
	return nil
}

func effectiveAppUserLimit(app models.App) int {
	if app.UserLimit > 0 {
		return app.UserLimit
	}
	return models.DefaultAppUserLimit
}

func effectiveAppRecordLimit(app models.App) int {
	if app.RecordLimit > 0 {
		return app.RecordLimit
	}
	return models.DefaultAppRecordLimit
}

func (s *Server) userCanManageAppLimits(user models.User) (bool, error) {
	if user.ID == 0 {
		return false, nil
	}

	var firstApp models.App
	err := s.db.Select("owner_id").Order("created_at ASC, id ASC").First(&firstApp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return firstApp.OwnerID == user.ID, nil
}

func (s *Server) clearSessionCookie(c *gin.Context, name string) {
	sameSite := http.SameSiteLaxMode
	if s.cfg.CookieSecure {
		sameSite = http.SameSiteNoneMode
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: sameSite,
	})
}

func (s *Server) deleteSessionFromCookie(c *gin.Context) {
	names := []string{s.cfg.SessionCookieName, s.adminSessionCookieName()}
	prefix := s.cfg.SessionCookieName + "_app_"
	for _, cookie := range c.Request.Cookies() {
		if strings.HasPrefix(cookie.Name, prefix) {
			names = append(names, cookie.Name)
		}
	}

	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		s.deleteSessionCookie(c, name)
	}
}

func (s *Server) deleteAdminSessionFromCookie(c *gin.Context) {
	s.deleteSessionCookie(c, s.adminSessionCookieName())
	s.deleteLegacySessionCookieIfAppID(c, adminSessionAppID)
}

func (s *Server) deleteAppSessionFromCookie(c *gin.Context, app models.App) {
	s.deleteSessionCookie(c, s.appSessionCookieName(app.ID))
	s.deleteLegacySessionCookieIfAppID(c, app.ID)
}

func (s *Server) deleteLegacySessionCookieIfAppID(c *gin.Context, appID uint) {
	token, err := c.Cookie(s.cfg.SessionCookieName)
	if err != nil || token == "" {
		return
	}
	session, _, ok := s.sessionFromToken(token)
	if ok && session.AppID == appID {
		s.db.Where("token_hash = ?", hashToken(token)).Delete(&models.Session{})
		s.clearSessionCookie(c, s.cfg.SessionCookieName)
	}
}

func (s *Server) deleteSessionCookie(c *gin.Context, name string) {
	token, err := c.Cookie(name)
	if err == nil && token != "" {
		s.db.Where("token_hash = ?", hashToken(token)).Delete(&models.Session{})
	}
	s.clearSessionCookie(c, name)
}

func (s *Server) sessionCookieName(appID uint) string {
	if appID == adminSessionAppID {
		return s.adminSessionCookieName()
	}
	return s.appSessionCookieName(appID)
}

func (s *Server) adminSessionCookieName() string {
	return s.cfg.SessionCookieName + "_admin"
}

func (s *Server) appSessionCookieName(appID uint) string {
	return s.cfg.SessionCookieName + "_app_" + strconv.FormatUint(uint64(appID), 10)
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
	return c.Request.URL.Path == "/admin" || strings.HasPrefix(c.Request.URL.Path, "/admin/")
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
