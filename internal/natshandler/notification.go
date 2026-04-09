package natshandler

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/helthtech/public-max-bot/internal/bot"
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
			log.Printf("nats notification.max unmarshal: %v", err)
			return
		}

		var payload notificationPayload
		if err := json.Unmarshal([]byte(notif.PayloadJSON), &payload); err != nil {
			log.Printf("nats notification.max payload unmarshal user=%s: %v", notif.UserID, err)
			return
		}

		chat, err := h.chatRepo.FindByUserID(context.Background(), notif.UserID)
		if err != nil {
			log.Printf("nats notification.max: chat not found for user %s: %v", notif.UserID, err)
			return
		}
		if chat == nil || chat.ChatID == nil {
			log.Printf("nats notification.max: no chat_id for user %s", notif.UserID)
			return
		}

		chatID, err := strconv.ParseInt(*chat.ChatID, 10, 64)
		if err != nil {
			log.Printf("nats notification.max: parse chat_id %s: %v", *chat.ChatID, err)
			return
		}

		text := bot.BuildNotificationMessage(payload.Title, payload.Body)
		if err := h.botClient.SendMessage(chatID, text, nil); err != nil {
			log.Printf("nats notification.max: send to chat %d user %s: %v", chatID, notif.UserID, err)
		} else {
			log.Printf("nats notification.max: sent to chat %d user %s type=%s", chatID, notif.UserID, notif.TemplateCode)
		}
	})
	return err
}
