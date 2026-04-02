package bot

import (
	"context"
)

func mainMenuKeyboard() *InlineKeyboard {
	return &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "📊 Мой прогресс", Payload: "menu:progress"}},
			{{Type: "callback", Text: "✅ Чеклист здоровья", Payload: "menu:checklist"}},
			{{Type: "callback", Text: "🔔 Уведомления", Payload: "menu:notifications"}},
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
	case "menu:checklist":
		h.handleChecklist(ctx, cb, chatID)
	case "menu:notifications":
		h.handleNotifications(ctx, chatID)
	case "menu:back":
		h.sendMainMenu(ctx, chatID)
	}
}

func (h *Handler) handleNotifications(_ context.Context, chatID int64) {
	_ = h.client.SendMessage(chatID,
		"🔔 **Уведомления**\n\n"+
			"Бот автоматически напомнит вам о необходимости внести данные "+
			"по показателям здоровья, когда подойдёт срок.\n\n"+
			"Уведомления включены по умолчанию.",
		&InlineKeyboard{
			Buttons: [][]Button{
				{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
			},
		},
	)
}
