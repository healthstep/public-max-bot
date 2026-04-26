package bot

import (
	"context"
	"log"
	"strconv"
)

func (h *Handler) handleUploadAnalyses(ctx context.Context, chatID int64) {
	text := "Загрузите PDF с анализами (до 5 файлов) в **личном кабинете** — раздел «Профиль»."
	if h.siteURL != "" {
		text += "\n\n[Открыть профиль](" + h.siteURL + "/profile)"
	}
	kb := &InlineKeyboard{Buttons: [][]Button{
		{{Type: "callback", Text: "◀️ Назад в меню", Payload: "menu:back"}},
	}}
	if err := h.client.SendMessage(chatID, text, kb); err != nil {
		log.Printf("handleUploadAnalyses chatID=%d: %v", chatID, err)
	}
}

func mainMenuKeyboard() *InlineKeyboard {
	return &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "📊 Мой прогресс", Payload: "menu:progress"}},
			{{Type: "callback", Text: "📅 Рекомендации недели", Payload: "menu:weekly_recs"}},
			{{Type: "callback", Text: "📄 Загрузить анализы", Payload: "menu:upload_analyses"}},
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
	case "menu:upload_analyses":
		h.handleUploadAnalyses(ctx, chatID)
	case "menu:back":
		h.sendMainMenu(ctx, chatID)
	case "menu:back_analysis":
		h.handleAddData(ctx, cb, chatID)
	}
}
