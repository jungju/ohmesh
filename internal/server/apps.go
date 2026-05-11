package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"ohmesh/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var appSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,78}[a-z0-9]$|^[a-z0-9]$`)

type appRequest struct {
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	DefaultRedirectURL string `json:"default_redirect_url"`
	Status             string `json:"status"`
}

type appDomainRequest struct {
	Domain    string `json:"domain"`
	IsPrimary bool   `json:"is_primary"`
}

func (s *Server) createApp(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	var req appRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}

	app, ok := appFromRequest(c, req)
	if !ok {
		return
	}
	app.OwnerID = admin.User.ID

	if err := s.db.Create(&app).Error; err != nil {
		respondError(c, http.StatusConflict, "app slug already exists")
		return
	}

	c.JSON(http.StatusCreated, app)
}

func (s *Server) listApps(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	var apps []models.App
	if err := s.db.Preload("Domains").Where("owner_id = ?", admin.User.ID).Order("created_at DESC").Find(&apps).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

func (s *Server) getApp(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	app, err := s.appBySlugForOwner(c.Param("slug"), admin.User.ID)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}
	if err := s.db.Preload("Domains").First(&app, app.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, app)
}

func (s *Server) updateApp(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	app, err := s.appBySlugForOwner(c.Param("slug"), admin.User.ID)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}

	var req appRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}

	updates := map[string]any{}
	if req.Name != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if req.DefaultRedirectURL != "" {
		redirectURL, err := normalizeBaseURL(req.DefaultRedirectURL)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid default_redirect_url")
			return
		}
		updates["default_redirect_url"] = redirectURL
	}
	if req.Status != "" {
		if !validAppStatus(req.Status) {
			respondError(c, http.StatusBadRequest, "invalid app status")
			return
		}
		updates["status"] = req.Status
	}

	if len(updates) > 0 {
		if err := s.db.Model(&app).Updates(updates).Error; err != nil {
			respondError(c, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	if err := s.db.Preload("Domains").First(&app, app.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, app)
}

func (s *Server) deleteApp(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	app, err := s.appBySlugForOwner(c.Param("slug"), admin.User.ID)
	if err != nil {
		respondDBError(c, err, "app not found")
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
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) createAppDomain(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	app, err := s.appBySlugForOwner(c.Param("slug"), admin.User.ID)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}

	var req appDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}

	domain, err := normalizeBaseURL(req.Domain)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid domain")
		return
	}

	appDomain := models.AppDomain{
		AppID:     app.ID,
		Domain:    domain,
		IsPrimary: req.IsPrimary,
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if req.IsPrimary {
			if err := tx.Model(&models.AppDomain{}).Where("app_id = ?", app.ID).Update("is_primary", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&appDomain).Error
	})
	if err != nil {
		respondError(c, http.StatusConflict, "domain already exists for app")
		return
	}

	c.JSON(http.StatusCreated, appDomain)
}

func (s *Server) listAppDomains(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	app, err := s.appBySlugForOwner(c.Param("slug"), admin.User.ID)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}

	var domains []models.AppDomain
	if err := s.db.Where("app_id = ?", app.ID).Order("is_primary DESC, created_at ASC").Find(&domains).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"domains": domains})
}

func (s *Server) deleteAppDomain(c *gin.Context) {
	admin, ok := s.requireAdminSession(c)
	if !ok {
		return
	}

	app, err := s.appBySlugForOwner(c.Param("slug"), admin.User.ID)
	if err != nil {
		respondDBError(c, err, "app not found")
		return
	}

	domainID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	result := s.db.Where("id = ? AND app_id = ?", domainID, app.ID).Delete(&models.AppDomain{})
	if result.Error != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	if result.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, "domain not found")
		return
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) appBySlug(slug string) (models.App, error) {
	var app models.App
	err := s.db.Where("slug = ?", slug).First(&app).Error
	return app, err
}

func (s *Server) appBySlugForOwner(slug string, ownerID uint) (models.App, error) {
	var app models.App
	err := s.db.Where("slug = ? AND owner_id = ?", slug, ownerID).First(&app).Error
	return app, err
}

func appFromRequest(c *gin.Context, req appRequest) (models.App, bool) {
	slug := strings.TrimSpace(req.Slug)
	name := strings.TrimSpace(req.Name)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = models.AppStatusActive
	}

	if !appSlugPattern.MatchString(slug) {
		respondError(c, http.StatusBadRequest, "invalid app slug")
		return models.App{}, false
	}
	if name == "" {
		respondError(c, http.StatusBadRequest, "app name is required")
		return models.App{}, false
	}
	if !validAppStatus(status) {
		respondError(c, http.StatusBadRequest, "invalid app status")
		return models.App{}, false
	}

	var redirectURL string
	if strings.TrimSpace(req.DefaultRedirectURL) != "" {
		normalized, err := normalizeBaseURL(req.DefaultRedirectURL)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid default_redirect_url")
			return models.App{}, false
		}
		redirectURL = normalized
	}

	return models.App{
		Slug:               slug,
		Name:               name,
		DefaultRedirectURL: redirectURL,
		Status:             status,
	}, true
}

func validAppStatus(status string) bool {
	return status == models.AppStatusActive || status == models.AppStatusDisabled
}

func normalizeRecordType(recordType string) (string, error) {
	recordType = strings.TrimSpace(recordType)
	if recordType == "" {
		return "", errors.New("record type is required")
	}
	if len(recordType) > 120 {
		return "", errors.New("record type is too long")
	}
	return recordType, nil
}
