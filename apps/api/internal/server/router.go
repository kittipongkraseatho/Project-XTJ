package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/config"
	authhttp "github.com/trading-journal/api/internal/httpapi/auth"
	mehttp "github.com/trading-journal/api/internal/httpapi/me"
	phase1http "github.com/trading-journal/api/internal/httpapi/phase1"
	phase2http "github.com/trading-journal/api/internal/httpapi/phase2"
	phase3http "github.com/trading-journal/api/internal/httpapi/phase3"
	phase4http "github.com/trading-journal/api/internal/httpapi/phase4"
	phase5http "github.com/trading-journal/api/internal/httpapi/phase5"
	phase6http "github.com/trading-journal/api/internal/httpapi/phase6"
	"github.com/trading-journal/api/internal/domain/broker_connection"
	aisvc "github.com/trading-journal/api/internal/integration/ai"
	whsvc "github.com/trading-journal/api/internal/integration/webhook"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/integration/billing"
	"github.com/trading-journal/api/internal/integration/broker"
	"github.com/trading-journal/api/internal/integration/mailer"
	"github.com/trading-journal/api/internal/integration/pdfreport"
	"github.com/trading-journal/api/internal/integration/cloudinary"
	"github.com/trading-journal/api/internal/integration/oauth"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/ratelimit"
	"github.com/trading-journal/api/internal/service"
	"github.com/trading-journal/api/internal/store"
	schedulesvc "github.com/trading-journal/api/internal/service/report_schedule"
	"github.com/trading-journal/api/internal/syncsvc"
	"github.com/trading-journal/api/internal/worker"
)

// RegisterRoutes mounts every endpoint under /v1 + the system endpoints.
//
// Phase 0 + Phase 1 endpoints (all real):
//
//   - GET  /healthz / /livez / /readyz
//   - POST /v1/auth/register         (public, IP-rate-limited)
//   - POST /v1/auth/login            (public, IP-rate-limited)
//   - POST /v1/auth/refresh          (public, IP-rate-limited)
//   - GET  /v1/auth/oauth/:provider/start     (public, IP-rate-limited)
//   - GET  /v1/auth/oauth/:provider/callback  (public)
//   - POST /v1/auth/logout           (auth required)
//   - GET  /v1/me                    (auth required)
//   - POST /v1/imports               (auth required, multipart, async via worker)
//   - GET  /v1/imports               (auth required)
//   - GET  /v1/accounts              (auth required)
//   - POST /v1/accounts              (auth required)
//   - GET  /v1/trades                (auth required)
//   - GET  /v1/trades/:id            (auth required)
//   - GET  /v1/trades/:id/journal    (auth required)
//   - PUT  /v1/trades/:id/journal    (auth required)
//   - GET  /v1/trades/:id/tags       (auth required)
//   - PUT  /v1/trades/:id/tags       (auth required)
//   - GET  /v1/trades/:id/images     (auth required)
//   - POST /v1/trades/:id/images     (auth required, multipart, Cloudinary)
//   - GET  /v1/tags                  (auth required)
//   - POST /v1/tags                  (auth required)
//   - DELETE /v1/tags/:id            (auth required)
//   - GET  /v1/analytics/overview     (auth required)
//   - GET  /v1/analytics/daily-pl     (auth required)
//   - GET  /v1/analytics/equity-curve (auth required)
//   - GET  /v1/analytics/symbol-breakdown (auth required, Phase 1 §30 advanced)
//
// Phase 2 endpoints (all real):
//
//   - GET    /v1/brokers/connections            (auth required)
//   - POST   /v1/brokers/connections            (auth required)
//   - GET    /v1/brokers/connections/:id        (auth required)
//   - POST   /v1/brokers/connections/:id/sync   (auth required)
//   - POST   /v1/brokers/connections/:id/disconnect (auth required)
//   - DELETE /v1/brokers/connections/:id        (auth required)
//   - GET    /v1/brokers/connections/:id/sync-runs (auth required)
//   - GET    /v1/brokers/sync-runs/:id          (auth required)
func RegisterRoutes(e *echo.Echo, pool *pgxpool.Pool, rdb *redis.Client, rl *observability.RateLimiter, cfg *config.Config, logger *slog.Logger, workerPool *worker.Pool) (*worker.SyncWorker, *worker.WebhookWorker) {
	// System endpoints (no auth, no rate limit)
	e.GET("/healthz", Healthz())
	e.GET("/livez", Healthz())
	e.GET("/readyz", Readyz(pool, rdb))

	// JWT signing key — loaded/generated on first boot (spec §7.2).
	jwtSvc, err := auth.NewJWTService(cfg.JWTKeyPath, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL)
	if err != nil {
		logger.Error("init jwt service", slog.String("error", err.Error()))
		panic(err)
	}

	st := store.New(pool)

	// OAuth providers (only those with both ID + secret are usable)
	registry := oauth.NewRegistry()
	if cfg.GoogleOAuthEnabled() {
		registry.Register(oauth.NewGoogleProvider(cfg.OAuthGoogleID, cfg.OAuthGoogleSecret, cfg.OAuthGoogleRedirect))
		logger.Info("oauth provider enabled", slog.String("provider", "google"))
	}
	if cfg.LineOAuthEnabled() {
		registry.Register(oauth.NewLineProvider(cfg.OAuthLineID, cfg.OAuthLineSecret, cfg.OAuthLineRedirect))
		logger.Info("oauth provider enabled", slog.String("provider", "line"))
	}

	authSvc := service.NewAuthService(cfg, st, jwtSvc, logger)
	authH := authhttp.New(authSvc, logger)
	meH := mehttp.New(authSvc, logger)
	oauthFlow := oauth.NewFlow(cfg, st, registry, jwtSvc, logger)
	oauthH := authhttp.NewOAuthHandlers(oauthFlow, logger)
	importSvc := service.NewImportService(st, logger)
	importSvc.AttachPool(workerPool) // bg async; nil-safe fallback

	// Phase 5.2 — webhook service + worker. The worker polls every 30s
	// and processes up to 50 deliveries per tick. ImportService emits
	// `import.completed` + `trade.created` events as it finishes a job.
	whSvcImpl := whsvc.NewServiceImpl(st)
	importSvc.AttachWebhookService(whSvcImpl)
	whWk := worker.NewWebhookWorker(context.Background(), st, whSvcImpl, logger, worker.WebhookWorkerOptions{
		PollInterval:   time.Duration(cfg.WebhookPollIntervalSeconds) * time.Second,
		BatchSize:      cfg.WebhookBatchSize,
		RequestTimeout: time.Duration(cfg.WebhookHTTPTimeoutSeconds) * time.Second,
})
	whWk.Start()

	// Phase 6 — AI analysis service + worker pool job.
	aiProvider, aiProviderErr := buildAIProvider(cfg, logger)
	if aiProviderErr != nil {
		logger.Warn("ai provider init failed — using stub", slog.String("err", aiProviderErr.Error()))
		aiProvider = aisvc.NewStubProvider()
	}
	aiSvcImpl := aisvc.NewServiceImpl(aisvc.ServiceConfig{
		Store:    aisvc.NewStoreAdapter(st),
		Provider: aiProvider,
		Log:      logger,
	})
	// Wire the enqueue callback AFTER aiSvcImpl is defined.
	aiSvcImpl.SetEnqueueAnalysis(func(id uuid.UUID) error {
		return workerPool.Enqueue(&worker.AnalysisJobPayload{
			AnalysisID: id,
			Handler: func(ctx context.Context) error {
				return aiSvcImpl.BuildAnalysis(ctx, id)
			},
		})
	})
	logger.Info("ai provider", slog.String("provider", aiProvider.Name()))

	var cldSvc cloudinary.Service = cloudinary.StubService{}
	if cfg.CloudinaryEnabled() {
		cs, err := cloudinary.New(cfg.CloudinaryCloudName, cfg.CloudinaryAPIKey, cfg.CloudinaryAPISecret)
		if err != nil {
			logger.Error("init cloudinary", slog.String("error", err.Error()))
			panic(err)
		}
		cldSvc = cs
		logger.Info("cloudinary enabled", slog.String("cloud_name", cfg.CloudinaryCloudName))
	} else {
		logger.Warn("cloudinary not configured (env CLOUDINARY_* missing) — image upload disabled")
	}

	p1 := phase1http.New(st, importSvc, cldSvc, logger)

	// Phase 2 — broker registry (mock-only) + SyncService + SyncWorker.
	brokerReg := broker.NewRegistry(map[broker_connection.Provider]broker.Provider{
		broker_connection.ProviderMock: broker.NewMockProvider(),
		// Phase 4: real MetaApi adapter plugged here behind env gate.
	})
	syncSvc := syncsvc.NewSyncService(st, brokerReg, logger)
	syncWk := worker.NewSyncWorker(context.Background(), st, workerPool, syncSvc, logger, worker.SyncWorkerOptions{
		PollInterval:  cfg.SyncPollInterval(), // 30s default
		PollBatchSize: 100,
	})
	syncWk.Start()
	p2 := phase2http.New(st, syncSvc, syncWk, brokerReg, logger)

	// Phase 3 — PDF reports. signintech impl by default; the stub is used
	// only in tests where the signintech code path is being explicitly avoided.
	var pdfImpl pdfreport.Service = pdfreport.NewSignintechService()
	p3 := phase3http.New(st, pdfImpl, logger)

	v1 := e.Group("/v1")

	// Public auth (rate-limited per IP)
	authPublic := v1.Group("/auth")
	authPublic.Use(rl.AuthIPLimit())
	authPublic.POST("/register", authH.Register)
	authPublic.POST("/login", authH.Login)
	authPublic.POST("/refresh", authH.Refresh)
	authPublic.GET("/oauth/:provider/start", oauthH.Start)
	authPublic.GET("/oauth/:provider/callback", oauthH.Callback)

	// Authed routes
	authed := v1.Group("", auth.AuthMiddleware(jwtSvc, logger))
	authed.Use(rl.UserLimit(), rl.TenantLimit())
	authed.POST("/auth/logout", authH.Logout)
	authed.GET("/me", meH.Me)

	// Phase 1 — imports / trades / analytics / accounts (auth required)
	authed.POST("/imports", p1.Upload)
	authed.GET("/imports", p1.ListImports)
	authed.GET("/accounts", p1.ListAccounts)
	authed.POST("/accounts", p1.CreateAccount)
	authed.GET("/trades", p1.ListTrades)
	authed.GET("/trades/:id", p1.GetTrade)
	authed.GET("/analytics/overview", p1.OverviewAnalytics)
	authed.GET("/analytics/daily-pl", p1.DailyPL)
	authed.GET("/analytics/equity-curve", p1.EquityCurve)
	authed.GET("/analytics/symbol-breakdown", p1.SymbolBreakdown)
	authed.GET("/analytics/session", p1.SessionBreakdown)
	authed.GET("/analytics/ea", p1.EABreakdown)
	authed.GET("/analytics/day-of-week", p1.DayOfWeekBreakdown)
	authed.GET("/analytics/hour-of-day", p1.HourOfDayBreakdown)

	// Phase 1 — journal, tags, images (auth + tenant scoped)
	authed.GET("/trades/:id/journal", p1.GetJournal)
	authed.PUT("/trades/:id/journal", p1.UpsertJournal)
	authed.GET("/trades/:id/tags", p1.ListTradeTags)
	authed.PUT("/trades/:id/tags", p1.SetTradeTags)
	authed.GET("/trades/:id/images", p1.ListImages)
	authed.POST("/trades/:id/images", p1.AttachImage)
	authed.GET("/tags", p1.ListTags)
	authed.POST("/tags", p1.CreateTag)
	authed.DELETE("/tags/:id", p1.DeleteTag)

	// Phase 2 — broker connections + sync (auth + tenant scoped)
	authed.GET("/brokers/connections", p2.ListConnections)
	authed.POST("/brokers/connections", p2.CreateConnection)
	authed.GET("/brokers/connections/:id", p2.GetConnection)
	authed.POST("/brokers/connections/:id/disconnect", p2.Disconnect)
	authed.DELETE("/brokers/connections/:id", p2.DeleteConnection)
	authed.POST("/brokers/connections/:id/sync", p2.TriggerSync)
	authed.GET("/brokers/connections/:id/sync-runs", p2.ListSyncRuns)
	authed.GET("/brokers/sync-runs/:id", p2.GetSyncRun)

	// Phase 3 — PDF reports (auth + tenant scoped)
	authed.POST("/reports/pdf", p3.GeneratePDF)

	// Phase 4 — billing (Stripe) + email scheduling (auth + tenant scoped)
	// Stripe vs Stub: env-gated. Empty STRIPE_SECRET_KEY → Stub.
	var billingSvc billing.Service = billing.NewStubService()
	if cfg.StripeBillingEnabled() {
		priceByPlan := map[string]string{
			"upload_journal": cfg.StripePriceUploadJournal,
			"starter":        cfg.StripePriceStarter,
			"pro":            cfg.StripePricePro,
		}
		s, err := billing.NewStripeService(billing.Config{
			SecretKey:     cfg.StripeSecretKey,
			WebhookSecret: cfg.StripeWebhookSecret,
			PriceByPlan:   priceByPlan,
		})
		if err != nil {
			logger.Warn("stripe init failed, falling back to stub", "err", err)
		} else {
			billingSvc = s
			logger.Info("stripe billing enabled")
		}
	} else {
		logger.Warn("stripe not configured (env STRIPE_SECRET_KEY missing) — using stub")
	}

	// Mailer: StubMailer if SMTP_HOST is empty, else SMTPMailer.
	var mailSvc mailer.Service = mailer.NewStubMailer("")
	if cfg.SMTPEnabled() {
		smtpMail, err := mailer.NewSMTPMailer(mailer.SMTPMailerConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom,
		})
		if err != nil {
			logger.Warn("smtp init failed, falling back to stub", "err", err)
		} else {
			mailSvc = smtpMail
			logger.Info("smtp mailer enabled")
		}
	}

	// Schedule worker — polls report_schedules every 60s.
	schedSvc := schedulesvc.NewScheduleService(st, logger)
	schedWk := worker.NewScheduleWorker(context.Background(), st, pdfImpl, mailSvc, logger, worker.ScheduleWorkerOptions{})
	schedWk.Start()
	p4 := phase4http.New(st, billingSvc, logger)
	p4s := phase4http.NewSchedulesHandler(st, schedSvc, logger)

	authed.GET("/billing/plans", p4.ListPlans)
	authed.POST("/billing/checkout", p4.Checkout)
	authed.GET("/billing/portal", p4.Portal)
	authed.GET("/billing/subscription", p4.CurrentSubscription)
	authed.POST("/billing/cancel", p4.Cancel)
	authed.GET("/billing/payments", p4.Payments)
	authed.GET("/reports/schedules", p4s.List)
	authed.POST("/reports/schedules", p4s.Create)
	authed.GET("/reports/schedules/:id", p4s.Get)
	authed.DELETE("/reports/schedules/:id", p4s.Delete)

	// Phase 4 — broker registry: register MetaApi if env-gated.
	if cfg.MetaApiEnabled() {
		brokerReg.Set(broker_connection.ProviderMetaApi,
			broker.NewMetaApiAdapter(broker.MetaApiConfig{
				Token: cfg.MetaAPIToken,
				Region: cfg.MetaAPIRegion,
				AccountID: cfg.MetaAPIAccountID,
			}))
		logger.Info("metaapi provider registered")
	}

	// Webhook is public — mount OUTSIDE the authed group.
	e.POST("/v1/billing/webhook", p4.Webhook)

	// Phase 5.1 — Public API rate-limit backend. Memory default, Redis for prod.
// Memory: per-instance counters (each replica counts independently).
// Redis: atomic Lua-based token bucket (correct across replicas).
	var apiKeyBucket ratelimit.KeyBucket
	if cfg.RateLimitRedisEnabled() {
		apiKeyBucket = ratelimit.NewRedisKeyBucket(
			rdb,
			cfg.RateLimitKeyPrefix,
			cfg.APIPublicRatePerMinute,
			cfg.APIPublicRateBurst,
			cfg.RateLimitTTLSeconds,
			logger,
		)
		logger.Info("rate limit backend",
			slog.String("backend", "redis"),
			slog.String("key_prefix", cfg.RateLimitKeyPrefix))
	} else {
		apiKeyBucket = ratelimit.NewMemoryKeyBucket(
			cfg.APIPublicRatePerMinute, cfg.APIPublicRateBurst)
		logger.Info("rate limit backend",
			slog.String("backend", "memory"))
	}
	apiKeySvc := apikey.NewServiceImpl(st)
	apiKeyAdmin := phase5http.NewAdminHandler(apiKeySvc, logger)
	authed.GET("/api-keys", apiKeyAdmin.List)
	authed.POST("/api-keys", apiKeyAdmin.Generate)
	authed.GET("/api-keys/:id", apiKeyAdmin.Get)
	authed.PATCH("/api-keys/:id", apiKeyAdmin.Update)
	authed.DELETE("/api-keys/:id", apiKeyAdmin.Revoke)

	// Phase 5.2 — webhook subscriptions (JWT-authed admin).
	whAdmin := phase6http.NewWebhookAdminHandler(whSvcImpl, st, logger)
	authed.GET("/webhooks", whAdmin.List)
	authed.POST("/webhooks", whAdmin.Create)
	authed.GET("/webhooks/:id", whAdmin.Get)
	authed.PATCH("/webhooks/:id", whAdmin.Update)
	authed.DELETE("/webhooks/:id", whAdmin.Revoke)
	authed.GET("/webhooks/:id/deliveries", whAdmin.Deliveries)

	// Phase 6 — AI analysis (JWT-authed).
	aiAdmin := phase6http.NewAIHandler(aiSvcImpl, st, logger)
	authed.POST("/trades/:id/analyze", aiAdmin.Analyze)
	authed.GET("/trades/:id/analysis", aiAdmin.GetAnalysis)
	authed.GET("/ai/usage", aiAdmin.GetUsage)
	phase5http.RegisterRoutes(e, &phase5http.PublicDeps{
		Logger:                   logger,
		AccountsList:             p1.ListAccounts,
		AccountsGet:              p1.GetAccount,
		TradesList:               p1.ListTrades,
		TradesGet:                p1.GetTrade,
		AnalyticsOverview:        p1.OverviewAnalytics,
		AnalyticsDailyPL:         p1.DailyPL,
		AnalyticsEquityCurve:     p1.EquityCurve,
		AnalyticsSymbolBreakdown: p1.SymbolBreakdown,
		AnalyticsSession:         p1.SessionBreakdown,
		AnalyticsEA:              p1.EABreakdown,
		AnalyticsDayOfWeek:       p1.DayOfWeekBreakdown,
		AnalyticsHourOfDay:       p1.HourOfDayBreakdown,
		BillingPayments:          p4.Payments,
	}, apiKeySvc, apiKeyBucket)
	logger.Info("public api registered",
		slog.String("rate_backend", cfg.RateLimitBackend),
		slog.Int("rate_per_minute", cfg.APIPublicRatePerMinute),
		slog.Int("rate_burst", cfg.APIPublicRateBurst))

	return syncWk, whWk
}

// buildAIProvider picks the LLM provider based on cfg.AIProvider.
// Errors are non-fatal — caller falls back to stub.
func buildAIProvider(cfg *config.Config, logger *slog.Logger) (aisvc.Provider, error) {
	switch cfg.AIProvider {
	case "openai":
		p, err := aisvc.NewOpenAIProvider(cfg.OpenAIKey, cfg.OpenAIModel, logger)
		if err != nil {
			return nil, err
		}
		return p, nil
	case "anthropic":
		p, err := aisvc.NewAnthropicProvider(cfg.AnthropicKey, cfg.AnthropicModel, logger)
		if err != nil {
			return nil, err
		}
		return p, nil
	default:
		return aisvc.NewStubProvider(), nil
	}
}