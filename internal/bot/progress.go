package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
)

func (h *Handler) handleProgress(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	resp, err := h.healthClient.GetDashboard(ctx, &healthpb.GetDashboardRequest{UserId: *chat.UserID})
	if err != nil {
		log.Printf("get dashboard for %s: %v", *chat.UserID, err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить прогресс. Попробуйте позже.", nil)
		return
	}

	bar := progressBar(resp.GetProgressPercent())
	text := fmt.Sprintf(
		"📊 **Мой прогресс**\n\n"+
			"Уровень: **%s**\n"+
			"%s %.0f%%\n"+
			"Заполнено: %d/%d показателей\n"+
			"Просрочено: %d\n",
		resp.GetLevel(),
		bar, resp.GetProgressPercent(),
		resp.GetFilledCriteria(), resp.GetTotalCriteria(),
		resp.GetOverdueCriteria(),
	)

	if len(resp.GetStates()) > 0 {
		text += "\n📋 **Детали:**\n"
		for _, s := range resp.GetStates() {
			icon := statusIcon(s.GetStatus())
			summary := s.GetLastValueSummary()
			if summary == "" {
				summary = "нет данных"
			}
			text += fmt.Sprintf("%s %s — %s\n", icon, s.GetCriterionName(), summary)
		}
	}

	_ = h.client.SendMessage(chatID, text, &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		},
	})
}

func (h *Handler) handleChecklist(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	resp, err := h.healthClient.GetUserCriterionStates(ctx, &healthpb.GetUserCriterionStatesRequest{UserId: *chat.UserID})
	if err != nil {
		log.Printf("get criterion states for %s: %v", *chat.UserID, err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить чеклист. Попробуйте позже.", nil)
		return
	}

	text := "✅ **Чеклист здоровья**\n\n"
	if len(resp.GetStates()) == 0 {
		text += "Пока нет данных о показателях."
	} else {
		for _, s := range resp.GetStates() {
			icon := statusIcon(s.GetStatus())
			rec := ""
			if s.GetRecommendation() != "" {
				rec = "\n   _" + s.GetRecommendation() + "_"
			}
			text += fmt.Sprintf("%s **%s** — %s%s\n", icon, s.GetCriterionName(), s.GetLastValueSummary(), rec)
		}
	}

	_ = h.client.SendMessage(chatID, text, &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		},
	})
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
	case "ok", "good":
		return "✅"
	case "warning", "overdue":
		return "⚠️"
	case "critical", "missing":
		return "❌"
	default:
		return "⬜"
	}
}
