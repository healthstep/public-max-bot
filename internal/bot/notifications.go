package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
)

// BuildNotificationMessage formats a notification for sending via the bot.
func BuildNotificationMessage(title, text string) string {
	if title == "" {
		return text
	}
	return fmt.Sprintf("🔔 **%s**\n\n%s", title, text)
}

// handleRecommendations shows all recommendations for the user.
func (h *Handler) handleRecommendations(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	resp, err := h.healthClient.GetRecommendations(ctx, &healthpb.GetRecommendationsRequest{
		UserId: *chat.UserID,
	})
	if err != nil {
		log.Printf("get recommendations for %s: %v", *chat.UserID, err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить рекомендации. Попробуйте позже.", nil)
		return
	}

	text := formatRecommendationsText(resp.GetRecommendations())

	_ = h.client.SendMessage(chatID, text, &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		},
	})
}

func formatRecommendationsText(recs []*healthpb.Recommendation) string {
	if len(recs) == 0 {
		return "💡 **Рекомендации**\n\n🎉 Всё отлично! Все показатели заполнены и в норме."
	}

	text := "💡 **Рекомендации**\n\n"
	for _, r := range recs {
		icon := severityIcon(r.GetSeverity())
		text += fmt.Sprintf("%s **%s**", icon, r.GetCriterionName())
		if r.GetAnalysisName() != "" {
			text += fmt.Sprintf(" (%s)", r.GetAnalysisName())
		}
		text += "\n"
		if r.GetText() != "" {
			text += fmt.Sprintf("   %s\n", r.GetText())
		}
		text += "\n"
	}
	return text
}

func severityIcon(severity string) string {
	switch severity {
	case "critical":
		return "🔴"
	case "warning":
		return "⚠️"
	case "ok":
		return "✅"
	default:
		return "💡"
	}
}
