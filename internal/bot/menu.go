package bot

import (
	"context"
	"strconv"

	"github.com/helthtech/public-max-bot/internal/obs"
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
		"В этот чат можно просто отправить **PDF** с анализами (до 5 подряд за 2 с) — разбор и подтверждение, как в личном кабинете на сайте. Пункт меню для этого не нужен.\n\n"+
			"🏥 **ЗдравоШаг** — ваш помощник по здоровью\n\nВыберите действие:",
		mainMenuKeyboard(),
	); err != nil {
		obs.BG("max").Error(err, "sendMainMenu", "chat_id", chatID)
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
