package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ohmesh/internal/config"
	"ohmesh/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHealth(t *testing.T) {
	router, _ := newTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebPagesRender(t *testing.T) {
	router, db := newTestRouter(t)

	app := models.App{
		Slug:               "notes",
		Name:               "Notes",
		DefaultRedirectURL: "https://example.com/notes",
		Status:             models.AppStatusActive,
	}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"/",
		"/login?app=notes&redirect_url=https://example.com/notes",
		"/admin/apps",
		"/admin/apps/notes",
		"/admin/apps/notes/users",
		"/admin/apps/notes/db",
	}

	for _, path := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "ohmesh") {
			t.Fatalf("%s did not render the ohmesh shell", path)
		}
	}
}

func TestRecordCRUDUsesCurrentUserAndApp(t *testing.T) {
	router, db := newTestRouter(t)

	app := models.App{
		Slug:   "notes",
		Name:   "Notes",
		Status: models.AppStatusActive,
	}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}

	user := models.User{Email: "user@example.com", Name: "User"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	token := "test-session-token"
	session := models.Session{
		UserID:    user.ID,
		AppID:     app.ID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"type":"note","data":{"title":"Hello","done":false}}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/apps/notes/records", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created models.AppRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.UserID != user.ID || created.AppID != app.ID || created.Type != "note" {
		t.Fatalf("created record is not scoped correctly: %#v", created)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/apps/notes/records/1", bytes.NewBufferString(`{"data":{"title":"Updated"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/apps/notes/records?type=note", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func newTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := models.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Addr:              ":0",
		DatabasePath:      ":memory:",
		SessionSecret:     "test-secret",
		SessionCookieName: "ohmesh_session",
		SessionTTL:        time.Hour,
	}

	return New(db, cfg), db
}
