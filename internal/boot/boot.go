package boot

import (
	"context"
	"fmt"
	"log"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-max-bot/internal/bot"
	"github.com/helthtech/public-max-bot/internal/migration"
	"github.com/helthtech/public-max-bot/internal/natshandler"
	"github.com/helthtech/public-max-bot/internal/repository"
	"github.com/nats-io/nats.go"
	"github.com/porebric/configs"
	"github.com/porebric/logger"
	"github.com/porebric/resty"
	restyerrors "github.com/porebric/resty/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Run(ctx context.Context) error {
	db, err := gorm.Open(postgres.Open(configs.Value(ctx, "db_dsn").String()), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	if err := migration.Migrate(db); err != nil {
		return fmt.Errorf("migration: %w", err)
	}

	usersConn, err := grpc.NewClient(
		configs.Value(ctx, "grpc_core_users").String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("grpc core-users: %w", err)
	}

	healthConn, err := grpc.NewClient(
		configs.Value(ctx, "grpc_core_health").String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("grpc core-health: %w", err)
	}

	usersClient := userspb.NewUserServiceClient(usersConn)
	healthClient := healthpb.NewHealthServiceClient(healthConn)

	nc, err := nats.Connect(configs.Value(ctx, "nats_url").String())
	if err != nil {
		return fmt.Errorf("nats: %w", err)
	}

	chatRepo := repository.NewChatRepository(db)
	botClient := bot.NewClient(configs.Value(ctx, "max_bot_token").String())

	webhookHost := configs.Value(ctx, "max_webhook_host").String()
	webhookURL := webhookHost + "/max/webhook"
	if err := botClient.SetWebhook(webhookURL); err != nil {
		return fmt.Errorf("set max webhook: %w", err)
	}
	log.Printf("max webhook set to %s", webhookURL)

	botHandler := bot.NewHandler(botClient, chatRepo, usersClient, healthClient, nc)

	notifHandler := natshandler.NewNotificationHandler(botClient, chatRepo)
	if err := notifHandler.Subscribe(nc); err != nil {
		return fmt.Errorf("nats subscribe notification.max: %w", err)
	}

	restyerrors.Init(nil)
	l := logger.New(logger.InfoLevel)
	router := resty.NewRouter(func() *logger.Logger { return l }, nil)

	resty.Endpoint(router, bot.NewWebhookRequest, botHandler.HandleWebhook)

	log.Println("public-max-bot starting")
	resty.RunServer(ctx, router, func(_ context.Context) error {
		usersConn.Close()
		healthConn.Close()
		nc.Close()
		return nil
	})

	return nil
}
