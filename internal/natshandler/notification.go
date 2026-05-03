package natshandler

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/helthtech/public-max-bot/internal/bot"
	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/helthtech/public-max-bot/internal/repository"
	"github.com/nats-io/nats.go"
)

// NotificationMessage matches the format published by core-health SendNotification.
type NotificationMessage struct {
	UserID           string `json:"user_id"`
	NotificationType string `json:"notification_type"`
	TemplateCode     string `json:"template_code"`
	PayloadJSON      string `json:"payload_json"`
}

type notificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type NotificationHandler struct {
	botClient *bot.Client
	chatRepo  *repository.ChatRepository
}

func NewNotificationHandler(botClient *bot.Client, chatRepo *repository.ChatRepository) *NotificationHandler {
	return &NotificationHandler{botClient: botClient, chatRepo: chatRepo}
}

func (h *NotificationHandler) Subscribe(nc *nats.Conn) error {
	_, err := nc.Subscribe("notification.max", func(msg *nats.Msg) {
		var notif NotificationMessage
		if err := json.Unmarshal(msg.Data, &notif); err != nil {
			obs.BG("nats").Error(err, "nats notification.max unmarshal")
			return
		}

		var payload notificationPayload
		if err := json.Unmarshal([]byte(notif.PayloadJSON), &payload); err != nil {
			obs.BG("nats").Error(err, "nats notification.max payload", "user_id", notif.UserID)
			return
		}

		chat, err := h.chatRepo.FindByUserID(context.Background(), notif.UserID)
		if err != nil {
			obs.BG("nats").Error(err, "nats notification.max: chat", "user_id", notif.UserID)
			return
		}
		if chat == nil || chat.ChatID == nil {
			obs.BG("nats").Warn("nats notification.max: no chat_id", "user_id", notif.UserID)
			return
		}

		chatID, err := strconv.ParseInt(*chat.ChatID, 10, 64)
		if err != nil {
			obs.BG("nats").Error(err, "nats notification.max: parse chat_id", "raw", *chat.ChatID)
			return
		}

		text := bot.BuildNotificationMessage(payload.Title, payload.Body)
		lg := obs.BG("nats")
		if err := h.botClient.SendMessage(chatID, text, nil); err != nil {
			lg.Error(err, "nats notification.max: send", "chat_id", chatID, "user_id", notif.UserID)
		} else {
			lg.Info("nats notification.max: sent", "chat_id", chatID, "user_id", notif.UserID, "type", notif.TemplateCode)
		}
	})
	return err
}
