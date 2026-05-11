package main

import (
	"log"

	"ohmesh/internal/config"
	"ohmesh/internal/models"
	"ohmesh/internal/server"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	cfg := config.Load()

	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := gorm.Open(sqlite.Open(cfg.DatabasePath), &gorm.Config{})
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := models.AutoMigrate(db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	router := server.New(db, cfg)

	log.Printf("ohmesh listening on %s", cfg.Addr)
	if err := router.Run(cfg.Addr); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
