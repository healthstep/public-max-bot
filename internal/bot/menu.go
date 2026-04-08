package bot

import (
	"context"
	"log"
	"strconv"
)

func mainMenuKeyboard() *InlineKeyboard {
	return &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "📊 Мой прогресс", Payload: "menu:progress"}},
			{{Type: "callback", Text: "📅 Рекомендации недели", Payload: "menu:weekly_recs"}},
		},
	}
}

func (h *Handler) sendMainMenu(ctx context.Context, chatID int64) {
	if err := h.client.SendMessage(chatID,
		"🏥 **ЗдравоШаг** — ваш помощник по здоровью\n\nВыберите действие:",
		mainMenuKeyboard(),
	); err != nil {
		log.Printf("sendMainMenu chatID=%d: %v", chatID, err)
	}
}

func (h *Handler) handleMenuCallback(ctx context.Context, cb *Callback, chatID int64) {
	switch cb.Payload {
	case "menu:add_data":
		h.handleAddData(ctx, cb, chatID)
	case "menu:progress":
		h.handleProgress(ctx, cb, chatID)
	case "menu:weekly_recs":
		maxUserID := strconv.FormatInt(cb.User.UserID, 10)
		h.handleWeeklyRecommendations(ctx, chatID, maxUserID)
	case "menu:back":
		h.sendMainMenu(ctx, chatID)
	case "menu:back_analysis":
		h.handleAddData(ctx, cb, chatID)
	}
}
