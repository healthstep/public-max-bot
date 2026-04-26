package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/porebric/logger"
)

func (h *Handler) handleProgress(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	h.clearMaxLabUpload(maxUserID)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	userID := *chat.UserID

	prog, err := h.healthClient.GetProgress(ctx, &healthpb.GetProgressRequest{UserId: userID})
	if err != nil {
		logger.Error(ctx, err, "get progress for user", "user_id", userID)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить прогресс. Попробуйте позже.", nil)
		return
	}

	criteria, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{UserId: userID})
	if err != nil {
		logger.Error(ctx, err, "get user criteria for user", "user_id", userID)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить данные. Попробуйте позже.", nil)
		return
	}

	text := formatProgressText(prog, criteria.GetEntries())

	_ = h.client.SendMessage(chatID, text, &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		},
	})
}

func formatProgressText(prog *healthpb.GetProgressResponse, entries []*healthpb.UserCriterionEntry) string {
	var b strings.Builder

	b.WriteString("📊 **Мой прогресс**\n\n")
	b.WriteString(fmt.Sprintf("Уровень: **%s**\n", prog.GetLevelLabel()))

	pct := prog.GetPercent()
	bar := progressBar(pct)
	b.WriteString(fmt.Sprintf("%s %.0f%%\n", bar, pct))
	b.WriteString(fmt.Sprintf("Заполнено: %d/%d критериев\n", prog.GetFilled(), prog.GetTotal()))

	if len(entries) == 0 {
		b.WriteString("\nДобавьте данные, нажав «➕ Добавить данные»!")
		return b.String()
	}

	b.WriteString("\n")
	for _, e := range entries {
		icon := statusIcon(e.GetStatus())
		b.WriteString(fmt.Sprintf("%s %s", icon, e.GetCriterionName()))
		if e.GetValue() != "" {
			b.WriteString(fmt.Sprintf(" — **%s**", e.GetValue()))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func progressBar(pct float64) string {
	filled := int(pct / 10)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

func statusIcon(status string) string {
	switch status {
	case "ok":
		return "✅"
	case "warning":
		return "⚠️"
	case "critical":
		return "🔴"
	case "empty", "":
		return "⚪"
	default:
		return "⚪"
	}
}
