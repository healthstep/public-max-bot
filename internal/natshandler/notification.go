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

type NotificationMessage struct {
	UserID string `json:"user_id"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Type   string `json:"type"`
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

		chat, err := h.chatRepo.FindByUserID(context.Background(), notif.UserID)
		if err != nil {
			log.Printf("find chat for user %s: %v", notif.UserID, err)
			return
		}
		if chat.ChatID == nil {
			return
		}

		chatID, err := strconv.ParseInt(*chat.ChatID, 10, 64)
		if err != nil {
			log.Printf("parse chat_id %s: %v", *chat.ChatID, err)
			return
		}

		text := bot.BuildNotificationMessage(notif.Title, notif.Text)
		if err := h.botClient.SendMessage(chatID, text, nil); err != nil {
			log.Printf("send notification to chat %d: %v", chatID, err)
		}
	})
	return err
}
