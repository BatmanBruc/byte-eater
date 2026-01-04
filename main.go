package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/config"
	"github.com/BatmanBruc/bat-bot-convetor/internal/converter"
	"github.com/BatmanBruc/bat-bot-convetor/internal/handlers"
	"github.com/BatmanBruc/bat-bot-convetor/internal/middleware"
	"github.com/BatmanBruc/bat-bot-convetor/internal/scheduler"
	"github.com/BatmanBruc/bat-bot-convetor/store"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func main() {
	_ = config.LoadEnvFile("config.env")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisDBStr := os.Getenv("REDIS_DB")
	redisDB := 0
	if redisDBStr != "" {
		var err error
		redisDB, err = strconv.Atoi(redisDBStr)
		if err != nil {
			log.Printf("Invalid REDIS_DB value, using default: 0")
			redisDB = 0
		}
	}

	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)
	rdb, err := store.NewRedisClient(redisAddr, redisPassword, redisDB, "bot_converter")
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()

	taskStore := store.NewRedisTaskStore(rdb, 24)
	userStateStore := store.NewRedisUserStore(rdb, 24)

	pgStore, err := store.NewPostgresStore(ctx, os.Getenv("POSTGRES_DSN"))
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}
	defer pgStore.Close()

	middlewares := middleware.NewMessageAnalyzer(pgStore)

	var h *handlers.Handlers

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		botToken = "YOUR_BOT_TOKEN_FROM_BOTFATHER"
		log.Println("Warning: Using default bot token. Set BOT_TOKEN environment variable.")
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Minute,
	}
	pollTimeout := 50 * time.Second

	b, err := bot.New(
		botToken,
		bot.WithHTTPClient(pollTimeout, httpClient),
	)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	conv := converter.NewDefaultConverter()

	taskScheduler := scheduler.NewScheduler(
		taskStore,
		conv,
		b,
		scheduler.Config{
			Workers: 3,
		},
	)

	h = handlers.NewHandlers(taskStore, userStateStore, taskScheduler, pgStore, pgStore)

	taskScheduler.Start()
	defer taskScheduler.Stop()

	handlerChain := middlewares.CheckTaskMiddleWare(
		middlewares.AnalyzeMessageMiddleware(
			h.MainHandler,
		),
	)

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.Message != nil
	}, handlerChain)

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, handlerChain)

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.PreCheckoutQuery != nil
	}, handlerChain)

	log.Println("Bot started. Press Ctrl+C to stop.")
	b.Start(ctx)
}
