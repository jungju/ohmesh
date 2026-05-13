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
	Name     string
	LoginURL string
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
		"appLoginPageURL": func(app models.App) string {
			return appPublicPageURL("/login", app)
		},
		"appLogoutPageURL": func(app models.App) string {
			return appPublicPageURL("/logout", app)
		},
		"appUserLimit": func(app models.App) int {
			return effectiveAppUserLimit(app)
		},
		"appRecordLimit": func(app models.App) int {
			return effectiveAppRecordLimit(app)
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
	if _, ok := data["AdminNavActive"]; !ok {
		data["AdminNavActive"] = adminNavActive(c)
	}
	if _, ok := data["DashboardNavActive"]; !ok {
		data["DashboardNavActive"] = c.Request.URL.Path == "/dashboard"
	}
	if _, ok := data["AdminUser"]; !ok {
		if value, exists := c.Get("adminUser"); exists {
			data["AdminUser"] = value
		} else if admin, ok := s.adminSessionFromCookie(c); ok {
			data["AdminUser"] = admin.User
		}
	}
	_, loggedIn := data["AdminUser"]
	data["IsLoggedIn"] = loggedIn
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
	s.render(c, http.StatusOK, "home.tmpl", gin.H{
		"Title": "ohmesh",
	})
}

func (s *Server) loginPage(c *gin.Context) {
	if strings.TrimSpace(c.Query("app")) != "" {
		s.appLoginPage(c)
		return
	}

	s.adminLoginPage(c)
}

func (s *Server) adminLoginPage(c *gin.Context) {
	next := safeAdminPath(c.Query("next"))
	if _, ok := s.adminSessionFromCookie(c); ok {
		c.Redirect(http.StatusSeeOther, next)
		return
	}

	redirectURL := absoluteAdminURL(c, next)
	providers := s.loginProviders(
		adminOAuthLoginURL("/auth/github/login", redirectURL),
		adminOAuthLoginURL("/auth/google/login", redirectURL),
	)

	s.render(c, http.StatusOK, "login.tmpl", gin.H{
		"Title":            "Login",
		"LoginHeading":     "ohmesh 로그인",
		"LoginDescription": "내 앱과 사용자 데이터 관리를 위해 ohmesh에 로그인합니다.",
		"Providers":        providers,
		"Next":             next,
	})
}

func (s *Server) appLoginPage(c *gin.Context) {
	app, redirectURL, err := s.resolveOAuthStartParams(c.Query("app"), c.Query("redirect_url"))
	if err != nil {
		status, message := oauthStartErrorStatus(err)
		s.renderErrorPage(c, status, "로그인 링크를 확인해주세요", message)
		return
	}

	if _, _, ok := s.appSessionFromCookie(c, app); ok {
		c.Redirect(http.StatusSeeOther, appendQuery(redirectURL, "ohmesh_login", "success"))
		return
	}

	providers := s.loginProviders(
		appOAuthLoginURL("/auth/github/login", app.Slug, redirectURL),
		appOAuthLoginURL("/auth/google/login", app.Slug, redirectURL),
	)

	s.render(c, http.StatusOK, "login.tmpl", gin.H{
		"Title":            app.Name + " Login",
		"LoginHeading":     app.Name + " 로그인",
		"LoginDescription": "쉬운 인증 서비스 ohmesh를 통해 " + app.Name + "에 로그인합니다.",
		"Providers":        providers,
		"App":              app,
		"RedirectURL":      redirectURL,
	})
}

func (s *Server) logoutPage(c *gin.Context) {
	app, redirectURL, err := s.resolveOAuthStartParams(c.Query("app"), c.Query("redirect_url"))
	if err != nil {
		status, message := oauthStartErrorStatus(err)
		s.renderErrorPage(c, status, "로그아웃 링크를 확인해주세요", message)
		return
	}

	s.deleteAppSessionFromCookie(c, app)
	c.Redirect(http.StatusSeeOther, appendQuery(redirectURL, "ohmesh_logout", "success"))
}

func (s *Server) loginProviders(githubLoginURL, googleLoginURL string) []loginProvider {
	providers := make([]loginProvider, 0, 2)
	if s.cfg.GitHubClientID != "" && s.cfg.GitHubClientSecret != "" {
		providers = append(providers, loginProvider{Name: "GitHub", LoginURL: githubLoginURL})
	}
	if s.cfg.GoogleClientID != "" && s.cfg.GoogleClientSecret != "" {
		providers = append(providers, loginProvider{Name: "Google", LoginURL: googleLoginURL})
	}
	return providers
}

func (s *Server) dashboardPage(c *gin.Context) {
	adminUser := adminUserFromContext(c)

	var appCount int64
	var activeAppCount int64
	var userCount int64
	var recordCount int64
	s.db.Model(&models.App{}).Where("owner_id = ?", adminUser.ID).Count(&appCount)
	s.db.Model(&models.App{}).Where("owner_id = ? AND status = ?", adminUser.ID, models.AppStatusActive).Count(&activeAppCount)
	s.db.Model(&models.AppRecord{}).
		Joins("JOIN apps ON apps.id = app_records.app_id").
		Where("apps.owner_id = ?", adminUser.ID).
		Count(&recordCount)
	s.db.Raw(`
		SELECT COUNT(*) FROM (
			SELECT sessions.user_id
			FROM sessions
			JOIN apps ON apps.id = sessions.app_id
			WHERE apps.owner_id = ?
			UNION
			SELECT app_records.user_id
			FROM app_records
			JOIN apps ON apps.id = app_records.app_id
			WHERE apps.owner_id = ?
		) AS app_users
	`, adminUser.ID, adminUser.ID).Scan(&userCount)

	var apps []models.App
	s.db.Preload("Domains").Where("owner_id = ?", adminUser.ID).Order("updated_at DESC").Limit(6).Find(&apps)

	var records []models.AppRecord
	s.db.Preload("User").
		Joins("JOIN apps ON apps.id = app_records.app_id").
		Where("apps.owner_id = ?", adminUser.ID).
		Order("app_records.updated_at DESC").
		Limit(6).
		Find(&records)

	s.render(c, http.StatusOK, "dashboard.tmpl", gin.H{
		"Title":          "Dashboard",
		"AppCount":       appCount,
		"ActiveAppCount": activeAppCount,
		"UserCount":      userCount,
		"RecordCount":    recordCount,
		"Apps":           apps,
		"Records":        records,
	})
}

func (s *Server) webLogout(c *gin.Context) {
	s.deleteAdminSessionFromCookie(c)
	redirectWithNotice(c, "/", "로그아웃했습니다.")
}

func (s *Server) webListApps(c *gin.Context) {
	s.renderAppManager(c, http.StatusOK, "", c.Query("app"))
}

func (s *Server) webCreateApp(c *gin.Context) {
	app, err := appFromForm(c)
	if err != nil {
		s.renderAppManager(c, http.StatusBadRequest, err.Error(), c.Query("app"))
		return
	}
	app.OwnerID = adminUserFromContext(c).ID

	if err := s.db.Create(&app).Error; err != nil {
		s.renderAppManager(c, http.StatusConflict, "이미 사용 중인 앱 slug입니다.", c.Query("app"))
		return
	}

	redirectWithNotice(c, appManagerPath(app.Slug), "앱을 만들었습니다.")
}

func (s *Server) webAppDetail(c *gin.Context) {
	s.renderAppManager(c, http.StatusOK, "", c.Param("slug"))
}

func (s *Server) webUpdateApp(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	updates := map[string]any{}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		s.renderAppManager(c, http.StatusBadRequest, "앱 이름이 필요합니다.", app.Slug)
		return
	}
	updates["name"] = name

	redirectURL := strings.TrimSpace(c.PostForm("default_redirect_url"))
	if redirectURL != "" {
		normalized, err := normalizeBaseURL(redirectURL)
		if err != nil {
			s.renderAppManager(c, http.StatusBadRequest, "기본 redirect URL이 올바르지 않습니다.", app.Slug)
			return
		}
		updates["default_redirect_url"] = normalized
	} else {
		updates["default_redirect_url"] = ""
	}

	status := strings.TrimSpace(c.PostForm("status"))
	if !validAppStatus(status) {
		s.renderAppManager(c, http.StatusBadRequest, "앱 상태가 올바르지 않습니다.", app.Slug)
		return
	}
	updates["status"] = status
	if raw := strings.TrimSpace(c.PostForm("user_limit")); raw != "" {
		userLimit, err := parseAppLimit(raw, models.DefaultAppUserLimit)
		if err != nil {
			s.renderAppManager(c, http.StatusBadRequest, "사용자 limit은 1 이상이어야 합니다.", app.Slug)
			return
		}
		updates["user_limit"] = userLimit
	}
	if raw := strings.TrimSpace(c.PostForm("record_limit")); raw != "" {
		recordLimit, err := parseAppLimit(raw, models.DefaultAppRecordLimit)
		if err != nil {
			s.renderAppManager(c, http.StatusBadRequest, "데이터 limit은 1 이상이어야 합니다.", app.Slug)
			return
		}
		updates["record_limit"] = recordLimit
	}

	if err := s.db.Model(&app).Updates(updates).Error; err != nil {
		s.renderAppManager(c, http.StatusInternalServerError, "앱을 저장하지 못했습니다.", app.Slug)
		return
	}

	redirectWithNotice(c, appManagerPath(app.Slug), "앱 설정을 저장했습니다.")
}

func (s *Server) webDeleteApp(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("app_id = ?", app.ID).Delete(&models.AppDomain{}).Error; err != nil {
			return err
		}
		if err := tx.Where("app_id = ?", app.ID).Delete(&models.Session{}).Error; err != nil {
			return err
		}
		if err := tx.Where("app_id = ?", app.ID).Delete(&models.AppRecord{}).Error; err != nil {
			return err
		}
		return tx.Delete(&app).Error
	})
	if err != nil {
		s.renderAppManager(c, http.StatusInternalServerError, "앱을 삭제하지 못했습니다.", app.Slug)
		return
	}

	redirectWithNotice(c, "/admin/apps", "앱을 삭제했습니다.")
}

func (s *Server) webCreateAppDomain(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	domain, err := normalizeBaseURL(c.PostForm("domain"))
	if err != nil {
		s.renderAppManager(c, http.StatusBadRequest, "도메인 URL이 올바르지 않습니다.", app.Slug)
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
		s.renderAppManager(c, http.StatusConflict, "이미 등록된 도메인입니다.", app.Slug)
		return
	}

	redirectWithNotice(c, appManagerPath(app.Slug), "도메인을 추가했습니다.")
}

func (s *Server) webDeleteAppDomain(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	domainID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	s.db.Where("id = ? AND app_id = ?", domainID, app.ID).Delete(&models.AppDomain{})
	redirectWithNotice(c, appManagerPath(app.Slug), "도메인을 삭제했습니다.")
}

func (s *Server) webAppUsers(c *gin.Context) {
	s.renderAppManager(c, http.StatusOK, "", c.Param("slug"))
}

func (s *Server) webExpireAppUserSessions(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}
	userID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	s.db.Where("app_id = ? AND user_id = ?", app.ID, userID).Delete(&models.Session{})
	redirectWithNotice(c, appManagerPath(app.Slug), "사용자의 앱 세션을 만료했습니다.")
}

func (s *Server) webAppDB(c *gin.Context) {
	s.renderAppManager(c, http.StatusOK, "", c.Param("slug"))
}

func (s *Server) webCreateAppRecord(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}

	userID, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("user_id")), 10, 64)
	if err != nil || userID == 0 {
		s.renderAppManager(c, http.StatusBadRequest, "user_id가 필요합니다.", app.Slug)
		return
	}

	recordType, err := normalizeRecordType(c.PostForm("type"))
	if err != nil {
		s.renderAppManager(c, http.StatusBadRequest, err.Error(), app.Slug)
		return
	}

	rawData := strings.TrimSpace(c.PostForm("data"))
	if !json.Valid([]byte(rawData)) {
		s.renderAppManager(c, http.StatusBadRequest, "data는 유효한 JSON이어야 합니다.", app.Slug)
		return
	}

	var user models.User
	if err := s.db.First(&user, uint(userID)).Error; err != nil {
		s.renderAppManager(c, http.StatusBadRequest, "존재하지 않는 user_id입니다.", app.Slug)
		return
	}

	record := models.AppRecord{
		AppID:  app.ID,
		UserID: uint(userID),
		Type:   recordType,
		Data:   models.JSONText(rawData),
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := ensureAppUserLimit(tx, uint(userID), app.ID); err != nil {
			return err
		}
		if err := ensureAppRecordLimit(tx, app); err != nil {
			return err
		}
		return tx.Create(&record).Error
	})
	if errors.Is(err, errAppUserLimitReached) {
		s.renderAppManager(c, http.StatusForbidden, "앱 사용자 limit에 도달했습니다.", app.Slug)
		return
	}
	if errors.Is(err, errAppRecordLimitReached) {
		s.renderAppManager(c, http.StatusForbidden, "앱 데이터 limit에 도달했습니다.", app.Slug)
		return
	}
	if err != nil {
		s.renderAppManager(c, http.StatusInternalServerError, "레코드를 만들지 못했습니다.", app.Slug)
		return
	}

	redirectWithNotice(c, appManagerPath(app.Slug), "레코드를 만들었습니다.")
}

func (s *Server) webUpdateAppRecord(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}
	recordID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	recordType, err := normalizeRecordType(c.PostForm("type"))
	if err != nil {
		s.renderAppManager(c, http.StatusBadRequest, err.Error(), app.Slug)
		return
	}

	rawData := strings.TrimSpace(c.PostForm("data"))
	if !json.Valid([]byte(rawData)) {
		s.renderAppManager(c, http.StatusBadRequest, "data는 유효한 JSON이어야 합니다.", app.Slug)
		return
	}

	result := s.db.Model(&models.AppRecord{}).
		Where("id = ? AND app_id = ?", recordID, app.ID).
		Updates(map[string]any{"type": recordType, "data": models.JSONText(rawData)})
	if result.Error != nil {
		s.renderAppManager(c, http.StatusInternalServerError, "레코드를 저장하지 못했습니다.", app.Slug)
		return
	}
	if result.RowsAffected == 0 {
		s.renderErrorPage(c, http.StatusNotFound, "Record not found", "레코드를 찾을 수 없습니다.")
		return
	}

	redirectWithNotice(c, appManagerPath(app.Slug), "레코드를 저장했습니다.")
}

func (s *Server) webDeleteAppRecord(c *gin.Context) {
	app, err := s.appBySlugForOwner(c.Param("slug"), adminUserFromContext(c).ID)
	if err != nil {
		s.renderErrorPage(c, http.StatusNotFound, "App not found", "앱을 찾을 수 없습니다.")
		return
	}
	recordID, ok := parseWebUint(c, "id")
	if !ok {
		return
	}

	s.db.Where("id = ? AND app_id = ?", recordID, app.ID).Delete(&models.AppRecord{})
	redirectWithNotice(c, appManagerPath(app.Slug), "레코드를 삭제했습니다.")
}

func (s *Server) renderAppManager(c *gin.Context, status int, errorMessage, selectedSlug string) {
	adminUser := adminUserFromContext(c)
	var apps []models.App
	s.db.Preload("Domains").Where("owner_id = ?", adminUser.ID).Order("name ASC, created_at DESC").Find(&apps)

	if selectedSlug == "" && len(apps) > 0 {
		selectedSlug = apps[0].Slug
	}

	data := gin.H{
		"Title":        "Apps",
		"Apps":         apps,
		"SelectedSlug": selectedSlug,
		"Error":        errorMessage,
		"Limit":        boundedIntQuery(c.Query("limit"), 100, 1, 500),
		"TypeFilter":   strings.TrimSpace(c.Query("type")),
		"UserIDFilter": strings.TrimSpace(c.Query("user_id")),
	}

	if selectedSlug != "" {
		app, err := s.appBySlugForOwner(selectedSlug, adminUser.ID)
		if err == nil {
			domains, _ := s.appDomains(app.ID)
			users, _ := s.appUsers(app.ID)
			recordUsers, _ := s.appRecordUsers(app.ID)
			records, _ := s.filteredAppRecords(c, app.ID)
			data["SelectedApp"] = app
			data["Domains"] = domains
			data["Users"] = users
			data["RecordUsers"] = recordUsers
			data["Records"] = records
			data["RunPrompt"] = appRunPrompt(docsBaseURL(c), app)
		} else if errorMessage == "" {
			data["Error"] = "선택한 앱을 찾을 수 없습니다."
		}
	}

	s.render(c, status, "admin_apps.tmpl", data)
}

func (s *Server) appDomains(appID uint) ([]models.AppDomain, error) {
	var domains []models.AppDomain
	err := s.db.Where("app_id = ?", appID).Order("is_primary DESC, created_at ASC").Find(&domains).Error
	return domains, err
}

func (s *Server) appUsers(appID uint) ([]appUserRow, error) {
	var users []appUserRow
	err := s.db.Raw(`
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
	`, appID, appID, appID, appID, appID, appID).Scan(&users).Error
	return users, err
}

func (s *Server) appRecordUsers(appID uint) ([]models.User, error) {
	var users []models.User
	err := s.db.Raw(`
		SELECT DISTINCT users.*
		FROM users
		WHERE EXISTS (SELECT 1 FROM sessions WHERE sessions.user_id = users.id AND sessions.app_id = ?)
		   OR EXISTS (SELECT 1 FROM app_records WHERE app_records.user_id = users.id AND app_records.app_id = ?)
		ORDER BY users.email ASC, users.id ASC
	`, appID, appID).Scan(&users).Error
	return users, err
}

func (s *Server) filteredAppRecords(c *gin.Context, appID uint) ([]models.AppRecord, error) {
	limit := boundedIntQuery(c.Query("limit"), 100, 1, 500)
	query := s.db.Preload("User").Where("app_id = ?", appID)

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
	err := query.Order("updated_at DESC").Limit(limit).Find(&records).Error
	return records, err
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
	userLimit, err := parseAppLimit(c.PostForm("user_limit"), models.DefaultAppUserLimit)
	if err != nil {
		return models.App{}, errors.New("사용자 limit은 1 이상이어야 합니다.")
	}
	recordLimit, err := parseAppLimit(c.PostForm("record_limit"), models.DefaultAppRecordLimit)
	if err != nil {
		return models.App{}, errors.New("데이터 limit은 1 이상이어야 합니다.")
	}

	return models.App{
		Slug:               slug,
		Name:               name,
		DefaultRedirectURL: redirectURL,
		Status:             status,
		UserLimit:          userLimit,
		RecordLimit:        recordLimit,
	}, nil
}

func parseAppLimit(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("app limit must be positive")
	}
	return value, nil
}

func adminOAuthLoginURL(path, redirectURL string) string {
	values := url.Values{}
	values.Set("admin", "1")
	values.Set("redirect_url", redirectURL)
	return path + "?" + values.Encode()
}

func appPublicPageURL(path string, app models.App) string {
	values := url.Values{}
	values.Set("app", app.Slug)
	if app.DefaultRedirectURL != "" {
		values.Set("redirect_url", app.DefaultRedirectURL)
	}
	return path + "?" + values.Encode()
}

func appRunPrompt(baseURL string, app models.App) string {
	redirectURL := app.DefaultRedirectURL
	if redirectURL == "" {
		redirectURL = "TODO: replace with this app's registered redirect URL"
	}

	return `You are working on the frontend app "` + app.Name + `".
Integrate ohmesh as this app's authentication service and user-scoped JSON storage API.

ohmesh base URL: ` + baseURL + `
App slug: ` + app.Slug + `
App name: ` + app.Name + `
Registered redirect URL: ` + redirectURL + `
App user limit: ` + strconv.Itoa(effectiveAppUserLimit(app)) + `
App record limit: ` + strconv.Itoa(effectiveAppRecordLimit(app)) + `
Machine-readable API guide: ` + baseURL + `/llms.txt
OpenAPI spec: ` + baseURL + `/openapi.json

Build the app so the user never handles tokens directly. ohmesh uses an app-specific HttpOnly session cookie. All browser fetch calls to ohmesh must include:

credentials: "include"

Authentication flow:
1. Add a login action that redirects the browser to:
   ` + baseURL + `/login?app=` + app.Slug + `&redirect_url={encodeURIComponent(current_app_url)}
2. Use the current page URL, without the hash fragment, as redirect_url. It must be inside the app's registered redirect URL.
3. After login, call:
   GET ` + baseURL + `/auth/me?app=` + app.Slug + `
   with credentials: "include".
4. If /auth/me returns 401, show the logged-out state. If it returns 200, show the authenticated app UI.

JSON record storage:
- Create a record:
  POST ` + baseURL + `/api/apps/` + app.Slug + `/records
  Content-Type: application/json
  credentials: "include"
  Body:
  {
    "type": "note",
    "data": {
      "title": "Hello",
      "done": false
    }
  }

- List records:
  GET ` + baseURL + `/api/apps/` + app.Slug + `/records?type=note&limit=100&offset=0

- Read one record:
  GET ` + baseURL + `/api/apps/` + app.Slug + `/records/{id}

- Update a record:
  PATCH ` + baseURL + `/api/apps/` + app.Slug + `/records/{id}
  Body may include "type", "data", or both.

- Delete a record:
  DELETE ` + baseURL + `/api/apps/` + app.Slug + `/records/{id}

Logout flow:
- Add a logout action that sends the user to:
  ` + baseURL + `/logout?app=` + app.Slug + `&redirect_url={encodeURIComponent(current_app_url)}

Important constraints:
- Do not use localStorage or visible tokens for ohmesh authentication.
- Do not expect OAuth access tokens, refresh tokens, or raw session tokens in any API response.
- Every data record is scoped to the current authenticated user and this app.
- This app currently allows up to ` + strconv.Itoa(effectiveAppUserLimit(app)) + ` users and ` + strconv.Itoa(effectiveAppRecordLimit(app)) + ` total JSON records.
- The "data" field can be any valid JSON value.
- The "type" field is required and must be 120 characters or less.
- Handle 401 by asking the user to log in again.
- Handle 403 as a wrong-app session.
- Keep the integration small and clear, using this app's existing UI and code style.`
}

func appOAuthLoginURL(path, appSlug, redirectURL string) string {
	values := url.Values{}
	values.Set("app", appSlug)
	values.Set("redirect_url", redirectURL)
	return path + "?" + values.Encode()
}

func adminUserFromContext(c *gin.Context) models.User {
	value, _ := c.Get("adminUser")
	user, _ := value.(models.User)
	return user
}

func appManagerPath(slug string) string {
	if slug == "" {
		return "/admin/apps"
	}
	values := url.Values{}
	values.Set("app", slug)
	return "/admin/apps?" + values.Encode()
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
	target, err := url.Parse(path)
	if err != nil {
		target = &url.URL{Path: "/admin/apps"}
	}
	values := target.Query()
	values.Set("notice", notice)
	target.RawQuery = values.Encode()
	c.Redirect(http.StatusSeeOther, target.String())
}
