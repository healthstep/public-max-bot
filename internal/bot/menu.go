package bot

import (
	"context"
)

func mainMenuKeyboard() *InlineKeyboard {
	return &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "📊 Мой прогресс", Payload: "menu:progress"}},
			{{Type: "callback", Text: "💡 Рекомендации", Payload: "menu:recommendations"}},
		},
	}
}

func (h *Handler) sendMainMenu(ctx context.Context, chatID int64) {
	_ = h.client.SendMessage(chatID,
		"🏥 **ЗдравоШаг** — ваш помощник по здоровью\n\nВыберите действие:",
		mainMenuKeyboard(),
	)
}

func (h *Handler) handleMenuCallback(ctx context.Context, cb *Callback, chatID int64) {
	switch cb.Payload {
	case "menu:add_data":
		h.handleAddData(ctx, cb, chatID)
	case "menu:progress":
		h.handleProgress(ctx, cb, chatID)
	case "menu:recommendations":
		h.handleRecommendations(ctx, cb, chatID)
	case "menu:back":
		h.sendMainMenu(ctx, chatID)
	case "menu:back_analysis":
		h.handleAddData(ctx, cb, chatID)
	}
}
