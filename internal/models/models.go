package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const (
	AppStatusActive   = "active"
	AppStatusDisabled = "disabled"

	IdentityProviderGitHub = "github"
	IdentityProviderGoogle = "google"
)

type JSONText []byte

func (j JSONText) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "null", nil
	}
	if !json.Valid(j) {
		return nil, errors.New("invalid JSON")
	}
	return string(j), nil
}

func (j *JSONText) Scan(value any) error {
	switch typed := value.(type) {
	case nil:
		*j = JSONText("null")
	case []byte:
		*j = append((*j)[0:0], typed...)
	case string:
		*j = append((*j)[0:0], typed...)
	default:
		return fmt.Errorf("scan JSONText: unsupported type %T", value)
	}

	if len(*j) == 0 {
		*j = JSONText("null")
	}
	if !json.Valid(*j) {
		return errors.New("scan JSONText: invalid JSON")
	}
	return nil
}

func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(j) {
		return nil, errors.New("invalid JSON")
	}
	return j, nil
}

func (j *JSONText) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) {
		return errors.New("invalid JSON")
	}
	*j = append((*j)[0:0], data...)
	return nil
}

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Email     string    `gorm:"index" json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Identity struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	User           User      `json:"-"`
	Provider       string    `gorm:"not null;size:40;uniqueIndex:idx_provider_user" json:"provider"`
	ProviderUserID string    `gorm:"not null;size:160;uniqueIndex:idx_provider_user" json:"provider_user_id"`
	AccessToken    string    `json:"-"`
	RefreshToken   string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type App struct {
	ID                 uint        `gorm:"primaryKey" json:"id"`
	OwnerID            uint        `gorm:"not null;default:0;index" json:"owner_id"`
	Owner              User        `json:"-"`
	Slug               string      `gorm:"not null;size:80;uniqueIndex" json:"slug"`
	Name               string      `gorm:"not null;size:160" json:"name"`
	DefaultRedirectURL string      `json:"default_redirect_url"`
	Status             string      `gorm:"not null;size:20;index" json:"status"`
	Domains            []AppDomain `json:"domains,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

type AppDomain struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	AppID     uint      `gorm:"not null;index;uniqueIndex:idx_app_domain" json:"app_id"`
	App       App       `json:"-"`
	Domain    string    `gorm:"not null;uniqueIndex:idx_app_domain" json:"domain"`
	IsPrimary bool      `gorm:"not null;default:false" json:"is_primary"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Session struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null;index" json:"user_id"`
	User      User      `json:"-"`
	AppID     uint      `gorm:"not null;index" json:"app_id"`
	App       App       `json:"-"`
	TokenHash string    `gorm:"not null;size:128;uniqueIndex" json:"-"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AppRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	AppID     uint      `gorm:"not null;index;index:idx_app_user_type" json:"app_id"`
	App       App       `json:"-"`
	UserID    uint      `gorm:"not null;index;index:idx_app_user_type" json:"user_id"`
	User      User      `json:"-"`
	Type      string    `gorm:"not null;size:120;index:idx_app_user_type" json:"type"`
	Data      JSONText  `gorm:"type:text;not null" json:"data"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{},
		&Identity{},
		&App{},
		&AppDomain{},
		&Session{},
		&AppRecord{},
	)
}
