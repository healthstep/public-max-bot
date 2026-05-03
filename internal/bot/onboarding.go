package bot

import (
	"context"
	"strconv"
	"strings"

	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/porebric/logger"
)

func (h *Handler) sendOnboarding(ctx context.Context, chatID int64) {
	text := "🏥 **Онбординг ЗдравоШаг**\n\n" +
		"Расскажите немного о себе, чтобы мы могли персонализировать рекомендации.\n\n" +
		"Укажите ваш пол:"

	kb := &InlineKeyboard{
		Buttons: [][]Button{
			{
				{Type: "callback", Text: "👨 Мужской", Payload: "onboard:sex:male"},
				{Type: "callback", Text: "👩 Женский", Payload: "onboard:sex:female"},
			},
			{
				{Type: "callback", Text: "⏭ Пропустить", Payload: "onboard:skip"},
			},
		},
	}
	_ = h.client.SendMessage(chatID, text, kb)
}

func (h *Handler) handleOnboardingCallback(ctx context.Context, cb *Callback, chatID int64) {
	payload := cb.Payload
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)

	switch {
	case strings.HasPrefix(payload, "onboard:sex:"):
		sex := strings.TrimPrefix(payload, "onboard:sex:")
		h.updateUserField(ctx, maxUserID, func(req *userspb.UpdateUserRequest) {
			req.Sex = &sex
		})
		_ = h.client.SendMessage(chatID,
			"Отлично! Теперь укажите дату рождения в формате ДД.ММ.ГГГГ\n(или нажмите «Пропустить»):",
			&InlineKeyboard{
				Buttons: [][]Button{
					{{Type: "callback", Text: "⏭ Пропустить", Payload: "onboard:skip_birth"}},
				},
			},
		)

	case payload == "onboard:skip_birth":
		h.finishOnboarding(ctx, maxUserID, chatID)

	case payload == "onboard:skip":
		h.finishOnboarding(ctx, maxUserID, chatID)

	case payload == "onboard:done":
		h.sendMainMenu(ctx, chatID)
	}
}

func (h *Handler) finishOnboarding(ctx context.Context, maxUserID string, chatID int64) {
	completed := true
	h.updateUserField(ctx, maxUserID, func(req *userspb.UpdateUserRequest) {
		req.OnboardingCompleted = &completed
	})

	_ = h.client.SendMessage(chatID,
		"Настройка завершена! 🎉\n\nТеперь вы можете пользоваться всеми функциями бота.",
		&InlineKeyboard{
			Buttons: [][]Button{
				{{Type: "callback", Text: "🏠 Главное меню", Payload: "onboard:done"}},
			},
		},
	)
}

func (h *Handler) updateUserField(ctx context.Context, maxUserID string, apply func(*userspb.UpdateUserRequest)) {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		return
	}
	req := &userspb.UpdateUserRequest{UserId: *chat.UserID}
	apply(req)
	if _, err := h.usersClient.UpdateUser(ctx, req); err != nil {
		logger.Error(ctx, err, "onboarding update user", "user_id", *chat.UserID)
	}
}
