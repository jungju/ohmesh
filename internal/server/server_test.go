package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

	admin := models.User{Email: "admin@example.com", Name: "Admin"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}

	app := models.App{
		OwnerID:            admin.ID,
		Slug:               "notes",
		Name:               "Notes",
		DefaultRedirectURL: "https://example.com/notes",
		Status:             models.AppStatusActive,
	}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}

	adminToken := createTestSession(t, db, admin.ID, adminSessionAppID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/apps", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin apps without login expected 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); !strings.HasPrefix(location, "/login?") {
		t.Fatalf("expected redirect to login, got %q", location)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("dashboard without login expected 303, got %d: %s", rec.Code, rec.Body.String())
	}

	tests := []string{
		"/",
		"/login?app=notes&redirect_url=https://example.com/notes",
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

	adminTests := []string{
		"/dashboard",
		"/admin/apps",
		"/admin/apps/notes",
		"/admin/apps/notes/users",
		"/admin/apps/notes/db",
	}

	for _, path := range adminTests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: adminToken})
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "ohmesh") {
			t.Fatalf("%s did not render the ohmesh shell", path)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/apps", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: adminToken})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin apps expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<dialog id="add-app-dialog"`) {
		t.Fatalf("admin apps should render add app modal: %s", body)
	}
	if strings.Contains(body, `<details class="add-app"`) {
		t.Fatalf("admin apps should not render add app disclosure: %s", body)
	}
	if !strings.Contains(body, `href="/login?app=notes&amp;redirect_url=https%3A%2F%2Fexample.com%2Fnotes"`) {
		t.Fatalf("admin apps should link to selected app login page: %s", body)
	}
}

func TestNavigationReflectsLoginState(t *testing.T) {
	router, db := newTestRouter(t)

	user := models.User{Email: "admin@example.com", Name: "Admin"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := createTestSession(t, db, user.ID, adminSessionAppID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	nav := navLinksHTML(t, rec.Body.String())
	if !strings.Contains(nav, `href="/login"`) {
		t.Fatalf("logged-out nav should include login: %s", nav)
	}
	if strings.Contains(nav, `href="/admin/apps"`) || strings.Contains(nav, `href="/login?next=%2Fadmin%2Fapps"`) {
		t.Fatalf("logged-out nav should hide app management: %s", nav)
	}
	if strings.Contains(nav, `aria-label="로그아웃"`) {
		t.Fatalf("logged-out nav should not include logout: %s", nav)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	nav = navLinksHTML(t, rec.Body.String())
	if strings.Contains(nav, `href="/login"`) {
		t.Fatalf("logged-in nav should hide login: %s", nav)
	}
	if !strings.Contains(nav, `href="/dashboard"`) {
		t.Fatalf("logged-in nav should include dashboard: %s", nav)
	}
	if !strings.Contains(nav, `href="/admin/apps"`) {
		t.Fatalf("logged-in nav should include app management: %s", nav)
	}
	if !strings.Contains(nav, `aria-label="로그아웃"`) {
		t.Fatalf("logged-in nav should include icon logout: %s", nav)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logged-in login page expected 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/dashboard" {
		t.Fatalf("logged-in login page should redirect to dashboard, got %q", location)
	}
}

func navLinksHTML(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `<div class="navlinks">`)
	if start == -1 {
		t.Fatalf("rendered page did not include navlinks: %s", body)
	}
	rest := body[start:]
	end := strings.Index(rest, `</div>`)
	if end == -1 {
		t.Fatalf("rendered page did not close navlinks: %s", body)
	}
	return rest[:end]
}

func TestAppLoginPageUsesAppOAuthFlow(t *testing.T) {
	router, db := newTestRouter(t)

	owner := models.User{Email: "owner@example.com", Name: "Owner"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	app := models.App{
		OwnerID:            owner.ID,
		Slug:               "notes",
		Name:               "Notes",
		DefaultRedirectURL: "https://example.com/notes",
		Status:             models.AppStatusActive,
	}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login?app=notes&redirect_url=https://example.com/notes/dashboard", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Notes 로그인") {
		t.Fatalf("app login page should show app name: %s", body)
	}
	if !strings.Contains(body, "앱 전용 ID") || !strings.Contains(body, "notes") {
		t.Fatalf("app login page should show app id: %s", body)
	}
	if !strings.Contains(body, `/auth/github/login?app=notes&amp;redirect_url=https%3A%2F%2Fexample.com%2Fnotes%2Fdashboard`) {
		t.Fatalf("app login page should link GitHub app OAuth flow: %s", body)
	}
	if strings.Contains(body, "admin=1") {
		t.Fatalf("app login page should not use admin OAuth flow: %s", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login?app=notes&redirect_url=https://evil.example/dashboard", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered redirect expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	user := models.User{Email: "user@example.com", Name: "User"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := createTestSession(t, db, user.ID, app.ID)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/login?app=notes&redirect_url=https://example.com/notes", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("existing app session expected 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "https://example.com/notes?ohmesh_login=success" {
		t.Fatalf("existing app session should return to app, got %q", location)
	}
}

func TestWebLogoutClearsSession(t *testing.T) {
	router, db := newTestRouter(t)

	user := models.User{Email: "admin@example.com", Name: "Admin"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := createTestSession(t, db, user.ID, adminSessionAppID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int64
	db.Model(&models.Session{}).Where("token_hash = ?", hashToken(token)).Count(&count)
	if count != 0 {
		t.Fatalf("expected logout to delete session, found %d", count)
	}
}

func TestAdminAppsAreScopedToLoggedInOwner(t *testing.T) {
	router, db := newTestRouter(t)

	owner := models.User{Email: "owner@example.com", Name: "Owner"}
	other := models.User{Email: "other@example.com", Name: "Other"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}

	ownedApp := models.App{OwnerID: owner.ID, Slug: "owned", Name: "Owned", Status: models.AppStatusActive}
	otherApp := models.App{OwnerID: other.ID, Slug: "other", Name: "Other", Status: models.AppStatusActive}
	if err := db.Create(&ownedApp).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherApp).Error; err != nil {
		t.Fatal(err)
	}

	token := createTestSession(t, db, owner.ID, adminSessionAppID)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/apps", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Owned") {
		t.Fatalf("owned app was not shown: %s", body)
	}
	if strings.Contains(body, "Other") {
		t.Fatalf("another owner's app was shown: %s", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/apps/other", nil)
	req.AddCookie(&http.Cookie{Name: "ohmesh_session", Value: token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("other owner's app expected 404, got %d: %s", rec.Code, rec.Body.String())
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
		Addr:               ":0",
		DatabasePath:       ":memory:",
		SessionSecret:      "test-secret",
		SessionCookieName:  "ohmesh_session",
		SessionTTL:         time.Hour,
		GitHubClientID:     "github-client",
		GitHubClientSecret: "github-secret",
		GoogleClientID:     "google-client",
		GoogleClientSecret: "google-secret",
	}

	return New(db, cfg), db
}

func createTestSession(t *testing.T, db *gorm.DB, userID, appID uint) string {
	t.Helper()

	token := "test-session-token-" + strconv.FormatUint(uint64(userID), 10) + "-" + strconv.FormatUint(uint64(appID), 10)
	session := models.Session{
		UserID:    userID,
		AppID:     appID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	return token
}
