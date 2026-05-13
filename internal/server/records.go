package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ohmesh/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type recordCreateRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (s *Server) listRecords(c *gin.Context) {
	auth, ok := s.requireAppSession(c)
	if !ok {
		return
	}

	limit := boundedIntQuery(c.Query("limit"), 100, 1, 500)
	offset := boundedIntQuery(c.Query("offset"), 0, 0, 100000)

	query := s.db.Where("app_id = ? AND user_id = ?", auth.App.ID, auth.User.ID)
	if recordType := strings.TrimSpace(c.Query("type")); recordType != "" {
		normalized, err := normalizeRecordType(recordType)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		query = query.Where("type = ?", normalized)
	}

	var records []models.AppRecord
	if err := query.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	c.JSON(http.StatusOK, gin.H{"records": records})
}

func (s *Server) createRecord(c *gin.Context) {
	auth, ok := s.requireAppSession(c)
	if !ok {
		return
	}

	var req recordCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}

	recordType, err := normalizeRecordType(req.Type)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	data, ok := validateJSONData(c, req.Data)
	if !ok {
		return
	}

	record := models.AppRecord{
		AppID:  auth.App.ID,
		UserID: auth.User.ID,
		Type:   recordType,
		Data:   models.JSONText(data),
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := ensureAppRecordLimit(tx, auth.App); err != nil {
			return err
		}
		return tx.Create(&record).Error
	})
	if errors.Is(err, errAppRecordLimitReached) {
		respondError(c, http.StatusForbidden, "app record limit reached")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	c.JSON(http.StatusCreated, record)
}

func (s *Server) getRecord(c *gin.Context) {
	auth, ok := s.requireAppSession(c)
	if !ok {
		return
	}

	record, ok := s.recordByID(c, auth)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, record)
}

func (s *Server) updateRecord(c *gin.Context) {
	auth, ok := s.requireAppSession(c)
	if !ok {
		return
	}

	record, ok := s.recordByID(c, auth)
	if !ok {
		return
	}

	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}

	updates := map[string]any{}
	if value, exists := raw["type"]; exists {
		var recordType string
		if err := json.Unmarshal(value, &recordType); err != nil {
			respondError(c, http.StatusBadRequest, "type must be a string")
			return
		}
		normalized, err := normalizeRecordType(recordType)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		updates["type"] = normalized
	}

	if value, exists := raw["data"]; exists {
		data, ok := validateJSONData(c, value)
		if !ok {
			return
		}
		updates["data"] = models.JSONText(data)
	}

	if len(updates) > 0 {
		if err := s.db.Model(&record).Updates(updates).Error; err != nil {
			respondError(c, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	if err := s.db.First(&record, record.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, record)
}

func (s *Server) deleteRecord(c *gin.Context) {
	auth, ok := s.requireAppSession(c)
	if !ok {
		return
	}

	recordID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	result := s.db.Where("id = ? AND app_id = ? AND user_id = ?", recordID, auth.App.ID, auth.User.ID).Delete(&models.AppRecord{})
	if result.Error != nil {
		respondError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	if result.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, "record not found")
		return
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) recordByID(c *gin.Context, auth authedContext) (models.AppRecord, bool) {
	recordID, ok := parseUintParam(c, "id")
	if !ok {
		return models.AppRecord{}, false
	}

	var record models.AppRecord
	err := s.db.Where("id = ? AND app_id = ? AND user_id = ?", recordID, auth.App.ID, auth.User.ID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "record not found")
			return models.AppRecord{}, false
		}
		respondError(c, http.StatusInternalServerError, "internal server error")
		return models.AppRecord{}, false
	}
	return record, true
}

func validateJSONData(c *gin.Context, data json.RawMessage) ([]byte, bool) {
	if len(data) == 0 {
		respondError(c, http.StatusBadRequest, "data is required")
		return nil, false
	}
	if !json.Valid(data) {
		respondError(c, http.StatusBadRequest, "data must be valid JSON")
		return nil, false
	}
	return data, true
}

func boundedIntQuery(raw string, fallback, minValue, maxValue int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
