// Command api runs the client portal HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/app"
	"clientportal/internal/config"
	"clientportal/internal/mail"
	"clientportal/internal/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.AppEnv == "production" && len(cfg.TrustedProxies) == 0 {
		log.Warn("TRUSTED_PROXIES is empty: behind a reverse proxy every client shares one rate-limit bucket")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	poolCfg.MaxConns = cfg.DBMaxConns
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	var mailer service.Mailer = mail.NewLog(log)
	if cfg.MailAPIKey != "" {
		mailer = mail.NewResend(cfg.MailAPIKey, cfg.MailFrom, "https://api.resend.com/emails")
	}

	gin.SetMode(gin.ReleaseMode)
	srv := &http.Server{
		Addr: ":" + cfg.Port,
		// nil gateway: the real Stripe adapter is not written yet (see PR notes), so the routes stay off.
		Handler:           app.New(pool, cfg, log, nil, mailer),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Places each recurring job as its date comes due; safe beside other instances (rows are locked).
	go func() {
		jobs := app.NewBookings(pool, cfg)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := jobs.GenerateDue(ctx); err != nil && ctx.Err() == nil {
					log.Error("recurring jobs", "err", err)
				}
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	log.Info("shutdown complete")
	return nil
}
