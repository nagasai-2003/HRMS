package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"hrms/backend/internal/api"
	"hrms/backend/internal/clientip"
	"hrms/backend/internal/mfa"
	"hrms/backend/migrations"
)

// Development only: never use it on a server.
const devMFAKey = "6465762d6f6e6c792d6d66612d6b65792d646f2d6e6f742d7573652d696e2d70"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dbURL := env("DATABASE_URL", "postgres://hrms:hrms-dev@localhost:5442/hrms?sslmode=disable")
	if err := migrateUp(dbURL); err != nil {
		log.Error("migrations", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	var rdb *redis.Client
	if u := env("REDIS_URL", "redis://localhost:6390/0"); u != "off" {
		opt, err := redis.ParseURL(u)
		if err != nil {
			log.Error("redis url", "err", err)
			os.Exit(1)
		}
		rdb = redis.NewClient(opt)
	}

	// Only these peers may tell us the client IP through X-Forwarded-For.
	trusted, err := clientip.Parse(env("TRUSTED_PROXIES", "127.0.0.1/32,::1/128"))
	if err != nil {
		log.Error("TRUSTED_PROXIES", "err", err)
		os.Exit(1)
	}
	prod := env("APP_ENV", "development") == "production"
	srv := api.New(pool, rdb, clientip.Resolver{Trusted: trusted}, prod, log)
	// MFA_KEY encrypts authenticator secrets. Development has a fixed key;
	// production must set its own and keep it with the backups.
	mfaKey := env("MFA_KEY", "")
	if mfaKey == "" {
		if prod {
			log.Error("MFA_KEY is required in production (openssl rand -hex 32)")
			os.Exit(1)
		}
		mfaKey = devMFAKey
		log.Warn("MFA_KEY not set: using the development key")
	}
	if srv.MFA, err = mfa.NewBox(mfaKey); err != nil {
		log.Error("MFA_KEY", "err", err)
		os.Exit(1)
	}
	srv.Issuer = env("MFA_ISSUER", "HR Workspace")
	srv.Origins = strings.Split(env("ALLOWED_ORIGINS", "localhost:3100,127.0.0.1:3100"), ",")
	go srv.RunJobs(ctx)
	go srv.Live.Run(ctx)

	hs := &http.Server{
		Addr:              env("HTTP_ADDR", ":8180"),
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hs.Shutdown(sh)
	}()
	log.Info("api listening", "addr", hs.Addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func migrateUp(dbURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5"+dbURL[len("postgres"):])
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
