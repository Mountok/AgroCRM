package main

import (
	"context"
	"log"
	"time"

	"agrocrm/backend/internal/config"
	"agrocrm/backend/internal/handlers"
	"agrocrm/backend/internal/httpx"
	"agrocrm/backend/internal/mailer"
	"agrocrm/backend/internal/store"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.Wait(ctx); err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		log.Fatal(err)
	}
	if err := db.Seed(); err != nil {
		log.Fatal(err)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), httpx.SecurityHeaders(), httpx.RequestSizeLimit(1<<20))
	r.Use(cors.New(cors.Config{
		AllowOriginFunc: cfg.IsAllowedOrigin,
		AllowMethods:    []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
		MaxAge:          12 * time.Hour,
	}))

	api := handlers.New(db, httpx.NewLimiter(), mailer.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.ApplicationsEmail))
	api.RegisterRoutes(r)

	log.Fatal(r.Run(":" + cfg.Port))
}
