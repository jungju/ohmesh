package server

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestMachineReadableDocsRender(t *testing.T) {
	router, _ := newTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "ohmesh.example.com")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("llms.txt expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("llms.txt should be text/plain, got %q", contentType)
	}
	body := rec.Body.String()
	for _, expected := range []string{
		"# ohmesh",
		"Base URL: https://ohmesh.example.com",
		"GET https://ohmesh.example.com/auth/me?app={app_slug}",
		"POST https://ohmesh.example.com/api/apps/{app_slug}/records",
		`credentials: "include"`,
		"Default app limits are 5 users and 10 total JSON records.",
		"Machine-readable OpenAPI spec: https://ohmesh.example.com/openapi.json",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("llms.txt missing %q: %s", expected, body)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "ohmesh.example.com")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi.json expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("openapi.json should be application/json, got %q", contentType)
	}

	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("openapi.json should parse: %v", err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Fatalf("unexpected OpenAPI version: %#v", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi.json missing paths: %#v", spec["paths"])
	}
	if _, ok := paths["/api/apps/{slug}/records"]; !ok {
		t.Fatalf("openapi.json should describe record collection endpoint: %#v", paths)
	}
	if _, ok := paths["/auth/me"]; !ok {
		t.Fatalf("openapi.json should describe auth session endpoint: %#v", paths)
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
	if !strings.Contains(body, `data-dialog-open="run-prompt-dialog"`) {
		t.Fatalf("admin apps should render run prompt button: %s", body)
	}
	if !strings.Contains(body, `name="user_limit" type="number" min="1" value="5"`) {
		t.Fatalf("admin apps should render default user limit field: %s", body)
	}
	if !strings.Contains(body, `name="record_limit" type="number" min="1" value="10"`) {
		t.Fatalf("admin apps should render default record limit field: %s", body)
	}
	if !strings.Contains(body, `<dialog id="run-prompt-dialog"`) {
		t.Fatalf("admin apps should render run prompt dialog: %s", body)
	}
	if !strings.Contains(body, `class="filter-form"`) {
		t.Fatalf("admin apps should render wide record filter form: %s", body)
	}
	if strings.Contains(body, "레코드 만들기") || strings.Contains(body, `action="/admin/apps/notes/db/records"`) {
		t.Fatalf("admin apps should not render manual record creation form: %s", body)
	}
	if !strings.Contains(body, `href="/login?app=notes&amp;redirect_url=https%3A%2F%2Fexample.com%2Fnotes"`) {
		t.Fatalf("admin apps should link to selected app login page: %s", body)
	}
	if !strings.Contains(body, `data-copy-value="/logout?app=notes&amp;redirect_url=https%3A%2F%2Fexample.com%2Fnotes"`) {
		t.Fatalf("admin apps should provide selected app logout URL copy button: %s", body)
	}
	for _, expected := range []string{
		"Integrate ohmesh as this app&#39;s authentication service",
		"App slug: notes",
		"Registered redirect URL: https://example.com/notes",
		"App user limit: 5",
		"App record limit: 10",
		"POST http://example.com/api/apps/notes/records",
		"OpenAPI spec: http://example.com/openapi.json",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("admin apps run prompt missing %q: %s", expected, body)
		}
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

func TestAdminSessionDoesNotOverwriteAppSession(t *testing.T) {
	router, db := newTestRouter(t)

	admin := models.User{Email: "admin@example.com", Name: "Admin"}
	appUser := models.User{Email: "user@example.com", Name: "User"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&appUser).Error; err != nil {
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
	appToken := createTestSession(t, db, appUser.ID, app.ID)
	adminCookie := &http.Cookie{Name: testSessionCookieName(adminSessionAppID), Value: adminToken}
	appCookie := &http.Cookie{Name: testSessionCookieName(app.ID), Value: appToken}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(adminCookie)
	req.AddCookie(appCookie)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin dashboard should use admin cookie, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/apps/notes/records", bytes.NewBufferString(`{"type":"note","data":{"title":"Hello"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	req.AddCookie(appCookie)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("app API should use app cookie even with admin cookie present, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/auth/me?app=notes", nil)
	req.AddCookie(adminCookie)
	req.AddCookie(appCookie)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth me should use requested app cookie, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"slug":"notes"`) {
		t.Fatalf("auth me should return app session: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/apps/notes/records", bytes.NewBufferString(`{"type":"note","data":{"title":"Blocked"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("app API should not accept admin cookie as app session, got %d: %s", rec.Code, rec.Body.String())
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
	if !strings.Contains(body, "쉬운 인증 서비스 ohmesh를 통해 Notes에 로그인합니다.") {
		t.Fatalf("app login page should show ohmesh marketing copy: %s", body)
	}
	if !strings.Contains(body, `/auth/github/login?app=notes&amp;redirect_url=https%3A%2F%2Fexample.com%2Fnotes%2Fdashboard`) {
		t.Fatalf("app login page should link GitHub app OAuth flow: %s", body)
	}
	if strings.Contains(body, "앱 로그인") || strings.Contains(body, "로그인 후 이동") {
		t.Fatalf("app login page should not show explanatory app panel: %s", body)
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

func TestLoginPageHidesUnconfiguredProviders(t *testing.T) {
	cfg := newTestConfig()
	router, db := newTestRouterWithConfig(t, cfg)

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
	req := httptest.NewRequest(http.MethodGet, "/login?app=notes&redirect_url=https://example.com/notes", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "GitHub") || strings.Contains(body, "Google") {
		t.Fatalf("unconfigured providers should not render: %s", body)
	}
	if strings.Contains(body, "GITHUB_CLIENT") || strings.Contains(body, "GOOGLE_CLIENT") {
		t.Fatalf("login page should not expose provider environment names: %s", body)
	}
	if !strings.Contains(body, "사용 가능한 로그인 방법이 없습니다.") {
		t.Fatalf("login page should show empty state when no providers are configured: %s", body)
	}
}

func TestAppLogoutURLUsesAppLogoutFlow(t *testing.T) {
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

	user := models.User{Email: "user@example.com", Name: "User"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := createTestSession(t, db, user.ID, app.ID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logout?app=notes&redirect_url=https://example.com/notes/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("app logout URL expected 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "https://example.com/notes/dashboard?ohmesh_logout=success" {
		t.Fatalf("app logout URL should return to app, got %q", location)
	}
	var count int64
	db.Model(&models.Session{}).Where("token_hash = ?", hashToken(token)).Count(&count)
	if count != 0 {
		t.Fatalf("expected app logout URL to delete session, found %d", count)
	}

	token = createTestSession(t, db, user.ID, app.ID)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/logout?app=notes&redirect_url=https://evil.example/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered redirect expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	db.Model(&models.Session{}).Where("token_hash = ?", hashToken(token)).Count(&count)
	if count != 1 {
		t.Fatalf("invalid app logout URL should keep session, found %d", count)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/logout?app=notes&redirect_url=https://example.com/notes", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("app logout expected 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "https://example.com/notes?ohmesh_logout=success" {
		t.Fatalf("app logout should return to app, got %q", location)
	}
	db.Model(&models.Session{}).Where("token_hash = ?", hashToken(token)).Count(&count)
	if count != 0 {
		t.Fatalf("expected app logout to delete session, found %d", count)
	}

	token = createTestSession(t, db, user.ID, app.ID)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/logout?app=notes&redirect_url=https://evil.example/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered logout redirect expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	db.Model(&models.Session{}).Where("token_hash = ?", hashToken(token)).Count(&count)
	if count != 1 {
		t.Fatalf("invalid app logout should keep session, found %d", count)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("API logout expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	db.Model(&models.Session{}).Where("token_hash = ?", hashToken(token)).Count(&count)
	if count != 0 {
		t.Fatalf("expected API logout to delete session, found %d", count)
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
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(adminSessionAppID), Value: token})
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

func TestAppLimitsAreEnforced(t *testing.T) {
	router, db := newTestRouter(t)

	app := models.App{
		Slug:        "notes",
		Name:        "Notes",
		Status:      models.AppStatusActive,
		UserLimit:   1,
		RecordLimit: 1,
	}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}

	user := models.User{Email: "user@example.com", Name: "User"}
	otherUser := models.User{Email: "other@example.com", Name: "Other"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherUser).Error; err != nil {
		t.Fatal(err)
	}

	token := createTestSession(t, db, user.ID, app.ID)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/apps/notes/records", bytes.NewBufferString(`{"type":"note","data":{"title":"First"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first record expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/apps/notes/records", bytes.NewBufferString(`{"type":"note","data":{"title":"Second"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: testSessionCookieName(app.ID), Value: token})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("second record should hit record limit, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "app record limit reached") {
		t.Fatalf("record limit response should explain the limit: %s", rec.Body.String())
	}

	server := &Server{db: db, cfg: newTestConfig()}
	existingUserCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	existingUserCtx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if err := server.createSession(existingUserCtx, user.ID, app.ID); err != nil {
		t.Fatalf("existing app user should be able to create a new session: %v", err)
	}

	newUserCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	newUserCtx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	err := server.createSession(newUserCtx, otherUser.ID, app.ID)
	if !errors.Is(err, errAppUserLimitReached) {
		t.Fatalf("new app user should hit user limit, got %v", err)
	}
}

func newTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	cfg := newTestConfig()
	cfg.GitHubClientID = "github-client"
	cfg.GitHubClientSecret = "github-secret"
	cfg.GoogleClientID = "google-client"
	cfg.GoogleClientSecret = "google-secret"
	return newTestRouterWithConfig(t, cfg)
}

func newTestRouterWithConfig(t *testing.T, cfg config.Config) (*gin.Engine, *gorm.DB) {
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

	return New(db, cfg), db
}

func newTestConfig() config.Config {
	return config.Config{
		Addr:              ":0",
		DatabasePath:      ":memory:",
		SessionSecret:     "test-secret",
		SessionCookieName: "ohmesh_session",
		SessionTTL:        time.Hour,
	}
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

func testSessionCookieName(appID uint) string {
	if appID == adminSessionAppID {
		return "ohmesh_session_admin"
	}
	return "ohmesh_session_app_" + strconv.FormatUint(uint64(appID), 10)
}
