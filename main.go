package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/api/handler"
	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/api/routes"
	"github.com/rizxfrog/oh-my-api/internal/config"
	"github.com/rizxfrog/oh-my-api/internal/db"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
	"github.com/rizxfrog/oh-my-api/internal/redis"
)

//go:embed all:frontend-dist
var frontendDist embed.FS

func main() {
	var configPath string
	var dbDSN string
	flag.StringVar(&configPath, "config", "./config.yaml", "path to config file")
	flag.StringVar(&dbDSN, "db", "", "database DSN (overrides config; use file path for SQLite or postgres://... for PostgreSQL)")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Database DSN: CLI flag overrides config
	dbDriver := cfg.Database.Driver
	dbDsn := cfg.Database.DSN
	if dbDSN != "" {
		dbDsn = dbDSN
		// Auto-detect driver from DSN
		if len(dbDSN) > 8 && dbDSN[:9] == "postgres:" {
			dbDriver = "postgres"
		} else {
			dbDriver = "sqlite"
		}
	}

	chinaSigner := proxy.NewSignatureEngine(proxy.SignatureOptions{
		CosyVersion: cfg.Lingma.CosyVersion,
	})
	credentials := proxy.NewCredentialManager(cfg.Credential, time.Now)
	accountStore := proxy.NewAccountStore(cfg.Credential, time.Now)
	accountPool := proxy.NewAccountPool(cfg.Account)
	balancer := proxy.NewRoundRobinBalancer()

	chinaTransport := proxy.NewNativeTransport(firstNonEmpty(cfg.Account.ChinaBaseURL, cfg.Lingma.BaseURL), chinaSigner, 90*time.Second)
	intlTransport := proxy.NewNativeTransport(cfg.Account.InternationalBaseURL, chinaSigner, 90*time.Second)
	sessions := proxy.NewSessionStore(time.Duration(cfg.Session.TTLMinutes)*time.Minute, cfg.Session.MaxSessions, time.Now)
	builder := proxy.NewBodyBuilder(cfg.Lingma.CosyVersion, time.Now, proxy.NewUUID, proxy.NewHexID)
	adapters := proxy.NewAdapterRegistry()
	chinaAdapter, err := proxy.NewChinaAdapter(chinaTransport, builder, time.Now)
	if err != nil {
		log.Fatalf("create china adapter: %v", err)
	}
	if err := adapters.Register(chinaAdapter); err != nil {
		log.Fatalf("register china adapter: %v", err)
	}
	intlAdapter, err := proxy.NewInternationalAdapter(intlTransport, builder, time.Now)
	if err != nil {
		log.Fatalf("create international adapter: %v", err)
	}
	if err := adapters.Register(intlAdapter); err != nil {
		log.Fatalf("register international adapter: %v", err)
	}
	models := proxy.NewAccountModelService(accountStore, accountPool, adapters, proxy.DefaultAliases(), time.Now)

	store, err := db.Open(dbDriver, dbDsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		log.Fatalf("migrate db: %v", err)
	}
	defer store.Close()

	// Initialize Redis for token stats
	var tokenStats *redis.TokenStats
	var requestStats *redis.RequestStats
	redisClient, err := redis.NewClient(cfg.Redis)
	if err != nil {
		log.Printf("WARNING: Redis unavailable, stats disabled: %v", err)
	} else {
		tokenStats = redis.NewTokenStats(redisClient)
		requestStats = redis.NewRequestStats(redisClient)
		defer redisClient.Close()
		log.Printf("Redis connected for stats")
	}

	// Start background log cleanup goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				settings, _ := store.GetSettings(context.Background())
				retentionDays := 30
				if d, err := strconv.Atoi(settings["retention_days"]); err == nil {
					retentionDays = d
				}

				// Update request timeout if configured
				if timeoutStr, ok := settings["request_timeout"]; ok && timeoutStr != "" {
					if sec, err := strconv.Atoi(timeoutStr); err == nil && sec > 0 {
						chinaTransport.SetTimeout(time.Duration(sec) * time.Second)
					}
				}

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				affected, err := store.CleanupExpiredLogs(ctx, retentionDays)
				cancel()
				if err != nil {
					log.Printf("cleanup logs error: %v", err)
				} else if affected > 0 {
					log.Printf("cleaned up %d expired log(s)", affected)
				}

				// Cleanup canonical execution records
				ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
				affected2, err2 := store.CleanupExpiredCanonicalRecords(ctx2, retentionDays)
				cancel2()
				if err2 != nil {
					log.Printf("cleanup canonical records error: %v", err2)
				} else if affected2 > 0 {
					log.Printf("cleaned up %d expired canonical record(s)", affected2)
				}
			}
		}
	}()

	bootstrapMgr := handler.NewBootstrapManager(
		cfg.Credential.AuthFile,
		firstNonEmpty(cfg.Lingma.OAuthCallbackAddr, cfg.Lingma.OAuthListenAddr),
		cfg.Lingma.CosyVersion,
	)
	bootstrapMgr.Accounts = accountStore
	bootstrapMgr.AutoDetectFreePort = true
	bootstrapMgr.OnCredentialSaved = func() {
		if _, err := accountStore.Refresh(context.Background()); err != nil {
			log.Printf("[bootstrap] account reload after save: %v", err)
		}
		if _, err := credentials.Refresh(context.Background()); err != nil {
			log.Printf("[bootstrap] credential reload after save: %v", err)
		}
		if err := models.Refresh(context.Background()); err != nil {
			log.Printf("[bootstrap] model refresh after save: %v", err)
		}
	}

	codebuddyClient := proxy.NewCodeBuddyClient(cfg.CodeBuddy.BaseURL)

	httpHandler := routes.New(model.Dependencies{
		Credentials:        credentials,
		Accounts:           accountStore,
		Models:             models,
		Sessions:           sessions,
		AccountPool:        accountPool,
		AccountConfig:      cfg.Account,
		Balancer:           balancer,
		Adapters:           adapters,
		Transport:          chinaTransport,
		Uploader:           chinaTransport,
		Builder:            builder,
		AdminToken:         cfg.Server.AdminToken,
		StoreExecutionLogs: cfg.Logging.StoreExecutionLogs,
		Now:                time.Now,
		FrontendFS:         frontendDist,
		TokenStats:         tokenStats,
		RequestStats:       requestStats,
		CodeBuddyConfig:    cfg.CodeBuddy,
	}, store, bootstrapMgr, codebuddyClient)

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go sweepSessions(ctx, sessions)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("lingma2api listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func sweepSessions(ctx context.Context, store *proxy.SessionStore) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = store.SweepExpired(context.Background())
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
