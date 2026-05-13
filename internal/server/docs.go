package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) llmsText(c *gin.Context) {
	baseURL := docsBaseURL(c)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, `# ohmesh

ohmesh is a DB-free auth and user-scoped JSON storage API for static apps.
Use this page when an automated client or LLM needs to understand how to write data through the ohmesh API.

Base URL: `+baseURL+`

## Core Flow

1. Register an app in the ohmesh admin UI and keep its app slug.
2. Send the user to:
   GET `+baseURL+`/login?app={app_slug}&redirect_url={encoded_app_url}
3. After OAuth succeeds, ohmesh redirects back to the registered redirect URL and sets the HttpOnly cookie named `+s.cfg.SessionCookieName+`.
4. Browser API calls must use credentials: "include" so the cookie is sent.
5. Data records are always scoped to the current session user and app.

## Check Login

GET `+baseURL+`/auth/me

JavaScript:
fetch("`+baseURL+`/auth/me", {
  credentials: "include"
})

If the user is logged in, the response includes user, app, and session.expires_at.

## Insert JSON Data

POST `+baseURL+`/api/apps/{app_slug}/records
Content-Type: application/json
Cookie auth: `+s.cfg.SessionCookieName+`

Body:
{
  "type": "note",
  "data": {
    "title": "Hello",
    "done": false
  }
}

JavaScript:
await fetch("`+baseURL+`/api/apps/{app_slug}/records", {
  method: "POST",
  credentials: "include",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    type: "note",
    data: { title: "Hello", done: false }
  })
})

Success response: 201 Created with a record object containing id, app_id, user_id, type, data, created_at, updated_at.

## Read And Update Data

GET `+baseURL+`/api/apps/{app_slug}/records?type=note&limit=100&offset=0
GET `+baseURL+`/api/apps/{app_slug}/records/{id}
PATCH `+baseURL+`/api/apps/{app_slug}/records/{id}
DELETE `+baseURL+`/api/apps/{app_slug}/records/{id}

Patch body may include type, data, or both.
Delete returns 204 No Content.

## Logout

App logout page:
GET `+baseURL+`/logout?app={app_slug}&redirect_url={encoded_app_url}

Programmatic app logout:
POST `+baseURL+`/auth/logout?app={app_slug}&redirect_url={encoded_app_url}

## Constraints

- redirect_url must be registered for the app.
- API requests require the ohmesh session cookie.
- Never expect OAuth access tokens, refresh tokens, or raw session tokens in API responses.
- data must be valid JSON.
- type is required and must be 120 characters or less.
- CORS requests must come from a registered app domain or allowed origin.

Machine-readable OpenAPI spec: `+baseURL+`/openapi.json
`)
}

func (s *Server) openapiJSON(c *gin.Context) {
	baseURL := docsBaseURL(c)
	c.JSON(http.StatusOK, gin.H{
		"openapi": "3.1.0",
		"info": gin.H{
			"title":       "ohmesh API",
			"version":     "1.0.0",
			"description": "Cookie-authenticated OAuth sessions and user-scoped JSON record storage for static apps.",
		},
		"servers": []gin.H{{"url": baseURL}},
		"tags": []gin.H{
			{"name": "Auth", "description": "OAuth session and cookie endpoints."},
			{"name": "Records", "description": "User-scoped JSON records for a registered app."},
		},
		"paths": gin.H{
			"/login": gin.H{
				"get": gin.H{
					"tags":        []string{"Auth"},
					"operationId": "startAppLogin",
					"summary":     "Start app OAuth login",
					"description": "Navigate the browser here with app and redirect_url. ohmesh completes OAuth, sets an HttpOnly session cookie, then returns to redirect_url.",
					"parameters": []gin.H{
						queryParam("app", "Registered app slug.", true),
						queryParam("redirect_url", "Registered URL to return to after login.", true),
					},
					"responses": gin.H{
						"200": gin.H{"description": "Login provider selection page."},
						"303": gin.H{"description": "Already logged in for this app; redirects to redirect_url."},
						"400": errorResponse("Invalid or unregistered redirect_url."),
						"404": errorResponse("App not found."),
					},
				},
			},
			"/logout": gin.H{
				"get": gin.H{
					"tags":        []string{"Auth"},
					"operationId": "showAppLogout",
					"summary":     "Show app logout confirmation page",
					"parameters": []gin.H{
						queryParam("app", "Registered app slug.", true),
						queryParam("redirect_url", "Registered URL to return to after logout.", true),
					},
					"responses": gin.H{
						"200": gin.H{"description": "Logout confirmation page."},
						"400": errorResponse("Invalid or unregistered redirect_url."),
						"404": errorResponse("App not found."),
					},
				},
			},
			"/auth/me": gin.H{
				"get": gin.H{
					"tags":        []string{"Auth"},
					"operationId": "getCurrentSession",
					"summary":     "Get current authenticated user and app session",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"responses": gin.H{
						"200": jsonResponse("Current session.", "MeResponse"),
						"401": errorResponse("Login required."),
					},
				},
			},
			"/auth/logout": gin.H{
				"post": gin.H{
					"tags":        []string{"Auth"},
					"operationId": "logout",
					"summary":     "Clear the ohmesh session cookie",
					"description": "Without app query parameters this returns 204. With app and redirect_url it redirects back with ohmesh_logout=success.",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"parameters": []gin.H{
						queryParam("app", "Registered app slug for app logout redirect.", false),
						queryParam("redirect_url", "Registered URL to return to after logout.", false),
					},
					"responses": gin.H{
						"204": gin.H{"description": "Session cleared."},
						"303": gin.H{"description": "Session cleared and redirected to app redirect_url."},
						"400": errorResponse("Invalid or unregistered redirect_url."),
					},
				},
			},
			"/api/apps/{slug}/records": gin.H{
				"get": gin.H{
					"tags":        []string{"Records"},
					"operationId": "listRecords",
					"summary":     "List records for the current user and app",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"parameters": []gin.H{
						pathParam("slug", "Registered app slug."),
						queryParam("type", "Optional record type filter.", false),
						intQueryParam("limit", "Maximum records to return. Default 100, max 500.", false),
						intQueryParam("offset", "Pagination offset. Default 0.", false),
					},
					"responses": gin.H{
						"200": jsonResponse("Record list.", "RecordListResponse"),
						"401": errorResponse("Login required."),
						"403": errorResponse("Session is not valid for this app."),
						"404": errorResponse("App not found."),
					},
				},
				"post": gin.H{
					"tags":        []string{"Records"},
					"operationId": "createRecord",
					"summary":     "Insert a user-scoped JSON record",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"parameters":  []gin.H{pathParam("slug", "Registered app slug.")},
					"requestBody": requestBody("RecordCreateRequest"),
					"responses": gin.H{
						"201": jsonResponse("Created record.", "Record"),
						"400": errorResponse("Invalid JSON body, missing type, or invalid data."),
						"401": errorResponse("Login required."),
						"403": errorResponse("Session is not valid for this app."),
						"404": errorResponse("App not found."),
					},
				},
			},
			"/api/apps/{slug}/records/{id}": gin.H{
				"get": gin.H{
					"tags":        []string{"Records"},
					"operationId": "getRecord",
					"summary":     "Get one user-scoped record",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"parameters":  []gin.H{pathParam("slug", "Registered app slug."), pathParam("id", "Record id.")},
					"responses": gin.H{
						"200": jsonResponse("Record.", "Record"),
						"401": errorResponse("Login required."),
						"403": errorResponse("Session is not valid for this app."),
						"404": errorResponse("App or record not found."),
					},
				},
				"patch": gin.H{
					"tags":        []string{"Records"},
					"operationId": "updateRecord",
					"summary":     "Update a record type and/or JSON data",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"parameters":  []gin.H{pathParam("slug", "Registered app slug."), pathParam("id", "Record id.")},
					"requestBody": requestBody("RecordUpdateRequest"),
					"responses": gin.H{
						"200": jsonResponse("Updated record.", "Record"),
						"400": errorResponse("Invalid JSON body, type, or data."),
						"401": errorResponse("Login required."),
						"403": errorResponse("Session is not valid for this app."),
						"404": errorResponse("App or record not found."),
					},
				},
				"delete": gin.H{
					"tags":        []string{"Records"},
					"operationId": "deleteRecord",
					"summary":     "Delete one user-scoped record",
					"security":    []gin.H{{"cookieAuth": []string{}}},
					"parameters":  []gin.H{pathParam("slug", "Registered app slug."), pathParam("id", "Record id.")},
					"responses": gin.H{
						"204": gin.H{"description": "Deleted."},
						"401": errorResponse("Login required."),
						"403": errorResponse("Session is not valid for this app."),
						"404": errorResponse("App or record not found."),
					},
				},
			},
		},
		"components": gin.H{
			"securitySchemes": gin.H{
				"cookieAuth": gin.H{
					"type": "apiKey",
					"in":   "cookie",
					"name": s.cfg.SessionCookieName,
				},
			},
			"schemas": gin.H{
				"Error": gin.H{
					"type":       "object",
					"required":   []string{"error"},
					"properties": gin.H{"error": gin.H{"type": "string"}},
				},
				"User": gin.H{
					"type":       "object",
					"properties": gin.H{"id": uintSchema(), "email": stringSchema(), "name": stringSchema(), "avatar_url": stringSchema()},
				},
				"AppRef": gin.H{
					"type":       "object",
					"properties": gin.H{"id": uintSchema(), "slug": stringSchema(), "name": stringSchema()},
				},
				"MeResponse": gin.H{
					"type": "object",
					"properties": gin.H{
						"user":    refSchema("User"),
						"app":     refSchema("AppRef"),
						"session": gin.H{"type": "object", "properties": gin.H{"expires_at": gin.H{"type": "string", "format": "date-time"}}},
					},
				},
				"RecordCreateRequest": gin.H{
					"type":     "object",
					"required": []string{"type", "data"},
					"properties": gin.H{
						"type": gin.H{"type": "string", "maxLength": 120},
						"data": anyJSONSchema(),
					},
				},
				"RecordUpdateRequest": gin.H{
					"type": "object",
					"properties": gin.H{
						"type": gin.H{"type": "string", "maxLength": 120},
						"data": anyJSONSchema(),
					},
				},
				"Record": gin.H{
					"type":     "object",
					"required": []string{"id", "app_id", "user_id", "type", "data", "created_at", "updated_at"},
					"properties": gin.H{
						"id":         uintSchema(),
						"app_id":     uintSchema(),
						"user_id":    uintSchema(),
						"type":       gin.H{"type": "string"},
						"data":       anyJSONSchema(),
						"created_at": gin.H{"type": "string", "format": "date-time"},
						"updated_at": gin.H{"type": "string", "format": "date-time"},
					},
				},
				"RecordListResponse": gin.H{
					"type":       "object",
					"properties": gin.H{"records": gin.H{"type": "array", "items": refSchema("Record")}},
				},
			},
		},
	})
}

func docsBaseURL(c *gin.Context) string {
	return strings.TrimRight(callbackURL(c, "/"), "/")
}

func queryParam(name, description string, required bool) gin.H {
	return gin.H{
		"name":        name,
		"in":          "query",
		"required":    required,
		"description": description,
		"schema":      stringSchema(),
	}
}

func intQueryParam(name, description string, required bool) gin.H {
	param := queryParam(name, description, required)
	param["schema"] = gin.H{"type": "integer", "minimum": 0}
	return param
}

func pathParam(name, description string) gin.H {
	return gin.H{
		"name":        name,
		"in":          "path",
		"required":    true,
		"description": description,
		"schema":      stringSchema(),
	}
}

func requestBody(schema string) gin.H {
	return gin.H{
		"required": true,
		"content": gin.H{
			"application/json": gin.H{"schema": refSchema(schema)},
		},
	}
}

func jsonResponse(description, schema string) gin.H {
	return gin.H{
		"description": description,
		"content": gin.H{
			"application/json": gin.H{"schema": refSchema(schema)},
		},
	}
}

func errorResponse(description string) gin.H {
	return gin.H{
		"description": description,
		"content": gin.H{
			"application/json": gin.H{"schema": refSchema("Error")},
		},
	}
}

func refSchema(name string) gin.H {
	return gin.H{"$ref": "#/components/schemas/" + name}
}

func stringSchema() gin.H {
	return gin.H{"type": "string"}
}

func uintSchema() gin.H {
	return gin.H{"type": "integer", "minimum": 1}
}

func anyJSONSchema() gin.H {
	return gin.H{"description": "Any valid JSON value."}
}
