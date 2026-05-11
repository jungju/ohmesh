package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ohmesh/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

//go:embed templates/*.tmpl
var templateFiles embed.FS

type loginProvider struct {
	Name          string
	LoginURL      string
	Enabled       bool
	MissingConfig string
}

type appUserRow struct {
	ID            uint
	Email         string
	Name          string
	AvatarURL     string
	SessionCount  int64
	RecordCount   int64
	LastSessionAt string
	LastRecordAt  string
}

func mustTemplates() *template.Template {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Local().Format("2006-01-02 15:04")
		},
		"jsonText": func(data models.JSONText) string {
			var out bytes.Buffer
			if err := json.Indent(&out, data, "", "  "); err == nil {
				return out.String()
			}
			return string(data)
		},
	}

	return template.Must(template.New("").Funcs(funcs).ParseFS(templateFiles, "templates/*.tmpl"))
}

func (s *Server) render(c *gin.Context, status int, name string, data gin.H) {
	if data == nil {
		data = gin.H{}
	}
	if _, ok := data["CurrentPath"]; !ok {
		data["CurrentPath"] = c.Request.URL.Path
	}
	if _, ok := data["Notice"]; !ok {
		data["Notice"] = c.Query("notice")
	}
	c.HTML(status, name, data)
}

func (s *Server) renderErrorPage(c *gin.Context, status int, title, message string) {
	s.render(c, status, "error.tmpl", gin.H{
		"Title":   title,
		"Message": message,
	})
}

func (s *Server) homePage(c *gin.Context) {
	var appCount int64
	var userCount int64
	var recordCount int64
	s.db.Model(&models.App{}).Count(&appCount)
	s.db.Model(&models.User{}).Count(&userCount)
	s.db.Model(&models.AppRecord{}).Count(&recordCount)

	var apps []models.App
	s.db.Preload("Domains").Order("created_at DESC").Limit(6).Find(&apps)

	s.render(c, http.StatusOK, "home.tmpl", gin.H{
		"Title":       "ohmesh",
		"AppCount":    appCount,
		"UserCount":   userCount,
		"RecordCount": recordCount,
		"Apps":        apps,
	})
}

func (s *Server) loginPage(c *gin.Context) {
	var apps []models.App
	s.db.Where("status = ?", models.AppStatusActive).Order("name ASC").Find(&apps)

	appSlug := strings.TrimSpace(c.Query("app"))
	redirectURL := strings.TrimSpace(c.Query("redirect_url"))

	var selectedApp models.App
	var loginError string
	appReady := false
	if appSlug != "" {
		app, err := s.loadActiveApp(appSlug)
		if err != nil {
			loginError = "선택한 앱을 찾을 수 없거나 비활성화되어 있습니다."
		} else {
			selectedApp = app
			if redirectURL == "" {
				redirectURL = app.DefaultRedirectURL
			}
			normalized, err := normalizeBaseURL(redirectURL)
			if err != nil {
				loginError = "redirect_url은 http 또는 https URL이어야 합니다."
			} else if !s.redirectAllowed(app, normalized) {
				loginError = "redirect_url이 앱에 등록된 URL과 일치하지 않습니다."
			} else {
				redirectURL = normalized
				appReady = true
			}
		}
	}

	providers := []loginProvider{
		{
			Name:          "GitHub",
			LoginURL:      oauthLoginURL("/auth/github/login", appSlug, redirectURL),
			Enabled:       appReady && s.cfg.GitHubClientID != "" && s.cfg.GitHubClientSecret != "",
			MissingConfig: "GITHUB_CLIENT_ID 또는 GITHUB_CLIENT_SECRET이 없습니다.",
		},
		{
			Name:          "Google",
			LoginURL:      oauthLoginURL("/auth/google/login", appSlug, redirectURL),
			Enabled:       appReady && s.cfg.GoogleClientID != "" && s.cfg.GoogleClientSecret != "",
			MissingConfig: "GOOGLE_CLIENT_ID 또는 GOOGLE_CLIENT_SECRET이 없습니다.",
		},
	}

	s.render(c, http.StatusOK, "login.tmpl", gin.H{
		"Title":       "Login",
		"Apps":        apps,
		"SelectedApp": selectedApp,
		"AppSlug":     appSlug,
		"RedirectURL": redirectURL,
		"Providers":   providers,
		"LoginError":  loginError,
		"AppReady":    appReady,
	})
}

func (s *Server) webListApps(c *gin.Context) {
	s.renderAppList(c, http.StatusOK, "")
}

func (s *Server) webCreateApp(c *gin.Context) {
	app, err := appFromForm(c)
	if err != nil {
		s.renderAppList(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.db.Create(&app).Error; err != nil {
		s.renderAppList(c, http.StatusConflict, "이미 사용 중인 앱 slug입니다.")
		return
	}

	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug), "앱을 만들었습니다.")
}

func (s *Server) webAppDetail(c *gin.Context) {
	s.renderAppDetail(c, http.StatusOK, "")
}

func (s *Server) webUpdateApp(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	updates := map[string]any{}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		s.renderAppDetail(c, http.StatusBadRequest, "앱 이름이 필요합니다.")
		return
	}
	updates["name"] = name

	redirectURL := strings.TrimSpace(c.PostForm("default_redirect_url"))
	if redirectURL != "" {
		normalized, err := normalizeBaseURL(redirectURL)
		if err != nil {
			s.renderAppDetail(c, http.StatusBadRequest, "기본 redirect URL이 올바르지 않습니다.")
			return
		}
		updates["default_redirect_url"] = normalized
	} else {
		updates["default_redirect_url"] = ""
	}

	status := strings.TrimSpace(c.PostForm("status"))
	if !validAppStatus(status) {
		s.renderAppDetail(c, http.StatusBadRequest, "앱 상태가 올바르지 않습니다.")
		return
	}
	updates["status"] = status

	if err := s.db.Model(&app).Updates(updates).Error; err != nil {
		s.renderAppDetail(c, http.StatusInternalServerError, "앱을 저장하지 못했습니다.")
		return
	}

	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug), "앱 설정을 저장했습니다.")
}

func (s *Server) webCreateAppDomain(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	domain, err := normalizeBaseURL(c.PostForm("domain"))
	if err != nil {
		s.renderAppDetail(c, http.StatusBadRequest, "도메인 URL이 올바르지 않습니다.")
		return
	}

	appDomain := models.AppDomain{
		AppID:     app.ID,
		Domain:    domain,
		IsPrimary: c.PostForm("is_primary") == "on",
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if appDomain.IsPrimary {
			if err := tx.Model(&models.AppDomain{}).Where("app_id = ?", app.ID).Update("is_primary", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&appDomain).Error
	})
	if err != nil {
		s.renderAppDetail(c, http.StatusConflict, "이미 등록된 도메인입니다.")
		return
	}

	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug), "도메인을 추가했습니다.")
}

func (s *Server) webDeleteAppDomain(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	domainID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	s.db.Where("id = ? AND app_id = ?", domainID, app.ID).Delete(&models.AppDomain{})
	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug), "도메인을 삭제했습니다.")
}

func (s *Server) webAppUsers(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	var users []appUserRow
	err = s.db.Raw(`
		SELECT
			users.id,
			users.email,
			users.name,
			users.avatar_url,
			(SELECT COUNT(*) FROM sessions WHERE sessions.user_id = users.id AND sessions.app_id = ?) AS session_count,
			(SELECT COUNT(*) FROM app_records WHERE app_records.user_id = users.id AND app_records.app_id = ?) AS record_count,
			(SELECT MAX(updated_at) FROM sessions WHERE sessions.user_id = users.id AND sessions.app_id = ?) AS last_session_at,
			(SELECT MAX(updated_at) FROM app_records WHERE app_records.user_id = users.id AND app_records.app_id = ?) AS last_record_at
		FROM users
		WHERE EXISTS (SELECT 1 FROM sessions WHERE sessions.user_id = users.id AND sessions.app_id = ?)
		   OR EXISTS (SELECT 1 FROM app_records WHERE app_records.user_id = users.id AND app_records.app_id = ?)
		ORDER BY users.updated_at DESC
	`, app.ID, app.ID, app.ID, app.ID, app.ID, app.ID).Scan(&users).Error
	if err != nil {
		s.renderErrorPage(c, http.StatusInternalServerError, "Users failed", "사용자 목록을 불러오지 못했습니다.")
		return
	}

	s.render(c, http.StatusOK, "admin_app_users.tmpl", gin.H{
		"Title": "App Users",
		"App":   app,
		"Users": users,
	})
}

func (s *Server) webExpireAppUserSessions(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}
	userID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	s.db.Where("app_id = ? AND user_id = ?", app.ID, userID).Delete(&models.Session{})
	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug)+"/users", "사용자의 앱 세션을 만료했습니다.")
}

func (s *Server) webAppDB(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	limit := boundedIntQuery(c.Query("limit"), 100, 1, 500)
	query := s.db.Preload("User").Where("app_id = ?", app.ID)

	typeFilter := strings.TrimSpace(c.Query("type"))
	if typeFilter != "" {
		query = query.Where("type = ?", typeFilter)
	}

	userIDFilter := strings.TrimSpace(c.Query("user_id"))
	if userIDFilter != "" {
		userID, err := strconv.ParseUint(userIDFilter, 10, 64)
		if err == nil && userID > 0 {
			query = query.Where("user_id = ?", uint(userID))
		}
	}

	var records []models.AppRecord
	if err := query.Order("updated_at DESC").Limit(limit).Find(&records).Error; err != nil {
		s.renderErrorPage(c, http.StatusInternalServerError, "Records failed", "레코드 목록을 불러오지 못했습니다.")
		return
	}

	var users []models.User
	s.db.Order("email ASC, id ASC").Find(&users)

	s.render(c, http.StatusOK, "admin_app_db.tmpl", gin.H{
		"Title":        "App DB",
		"App":          app,
		"Records":      records,
		"Users":        users,
		"TypeFilter":   typeFilter,
		"UserIDFilter": userIDFilter,
		"Limit":        limit,
	})
}

func (s *Server) webCreateAppRecord(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	userID, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("user_id")), 10, 64)
	if err != nil || userID == 0 {
		s.renderErrorPage(c, http.StatusBadRequest, "Invalid user", "user_id가 필요합니다.")
		return
	}

	recordType, err := normalizeRecordType(c.PostForm("type"))
	if err != nil {
		s.renderErrorPage(c, http.StatusBadRequest, "Invalid type", err.Error())
		return
	}

	rawData := strings.TrimSpace(c.PostForm("data"))
	if !json.Valid([]byte(rawData)) {
		s.renderErrorPage(c, http.StatusBadRequest, "Invalid JSON", "data는 유효한 JSON이어야 합니다.")
		return
	}

	var user models.User
	if err := s.db.First(&user, uint(userID)).Error; err != nil {
		s.renderErrorPage(c, http.StatusBadRequest, "Invalid user", "존재하지 않는 user_id입니다.")
		return
	}

	record := models.AppRecord{
		AppID:  app.ID,
		UserID: uint(userID),
		Type:   recordType,
		Data:   models.JSONText(rawData),
	}
	if err := s.db.Create(&record).Error; err != nil {
		s.renderErrorPage(c, http.StatusInternalServerError, "Create failed", "레코드를 만들지 못했습니다.")
		return
	}

	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug)+"/db", "레코드를 만들었습니다.")
}

func (s *Server) webDeleteAppRecord(c *gin.Context) {
	app, err := s.appBySlug(c.Param("slug"))
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}
	recordID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	s.db.Where("id = ? AND app_id = ?", recordID, app.ID).Delete(&models.AppRecord{})
	redirectWithNotice(c, "/admin/apps/"+url.PathEscape(app.Slug)+"/db", "레코드를 삭제했습니다.")
}

func (s *Server) renderAppList(c *gin.Context, status int, errorMessage string) {
	var apps []models.App
	s.db.Preload("Domains").Order("created_at DESC").Find(&apps)

	s.render(c, status, "admin_apps.tmpl", gin.H{
		"Title": "Apps",
		"Apps":  apps,
		"Error": errorMessage,
	})
}

func (s *Server) renderAppDetail(c *gin.Context, status int, errorMessage string) {
	app, err := s.appBySlug(c.Param("slug"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}
	if err != nil {
		s.renderErrorPage(c, http.StatusInternalServerError, "App failed", "앱 정보를 불러오지 못했습니다.")
		return
	}

	var domains []models.AppDomain
	s.db.Where("app_id = ?", app.ID).Order("is_primary DESC, created_at ASC").Find(&domains)

	s.render(c, status, "admin_app_detail.tmpl", gin.H{
		"Title":   "App Detail",
		"App":     app,
		"Domains": domains,
		"Error":   errorMessage,
	})
}

func appFromForm(c *gin.Context) (models.App, error) {
	slug := strings.TrimSpace(c.PostForm("slug"))
	name := strings.TrimSpace(c.PostForm("name"))
	redirectURL := strings.TrimSpace(c.PostForm("default_redirect_url"))
	status := strings.TrimSpace(c.PostForm("status"))
	if status == "" {
		status = models.AppStatusActive
	}

	if !appSlugPattern.MatchString(slug) {
		return models.App{}, errors.New("앱 slug는 소문자, 숫자, 하이픈만 사용할 수 있습니다.")
	}
	if name == "" {
		return models.App{}, errors.New("앱 이름이 필요합니다.")
	}
	if !validAppStatus(status) {
		return models.App{}, errors.New("앱 상태가 올바르지 않습니다.")
	}

	if redirectURL != "" {
		normalized, err := normalizeBaseURL(redirectURL)
		if err != nil {
			return models.App{}, errors.New("기본 redirect URL이 올바르지 않습니다.")
		}
		redirectURL = normalized
	}

	return models.App{
		Slug:               slug,
		Name:               name,
		DefaultRedirectURL: redirectURL,
		Status:             status,
	}, nil
}

func oauthLoginURL(path, appSlug, redirectURL string) string {
	values := url.Values{}
	values.Set("app", appSlug)
	values.Set("redirect_url", redirectURL)
	return path + "?" + values.Encode()
}

func parseWebUint(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		c.AbortWithStatus(http.StatusBadRequest)
		return 0, false
	}
	return uint(value), true
}

func redirectWithNotice(c *gin.Context, path, notice string) {
	values := url.Values{}
	values.Set("notice", notice)
	c.Redirect(http.StatusSeeOther, path+"?"+values.Encode())
}
