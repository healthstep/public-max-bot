package bot

import (
	"context"
	"fmt"
	"strconv"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/porebric/logger"
)

// BuildNotificationMessage formats a notification for sending via the bot.
func BuildNotificationMessage(title, text string) string {
	if title == "" {
		return text
	}
	return fmt.Sprintf("🔔 **%s**\n\n%s", title, text)
}

// handleWeeklyRecommendations shows the user's weekly recommendation plan.
func (h *Handler) handleWeeklyRecommendations(ctx context.Context, chatID int64, maxUserID string) {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	resp, err := h.healthClient.GetWeeklyRecommendations(ctx, &healthpb.GetWeeklyRecommendationsRequest{
		UserId: *chat.UserID,
	})
	if err != nil {
		logger.Error(ctx, err, "get weekly recommendations for user", "user_id", *chat.UserID)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить рекомендации на неделю. Попробуйте позже.", nil)
		return
	}

	text := formatWeeklyRecommendationsText(resp)

	_ = h.client.SendMessage(chatID, text, &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		},
	})
}

func formatWeeklyRecommendationsText(resp *healthpb.GetWeeklyRecommendationsResponse) string {
	header := fmt.Sprintf("📅 **Рекомендации на неделю** (с %s)\n\n", resp.GetWeekStart())
	items := resp.GetItems()
	if len(items) == 0 {
		return header + "🎉 На эту неделю рекомендаций нет — все показатели в норме!"
	}

	text := header
	for _, item := range items {
		icon := recTypeIconMax(item.GetType())
		title := item.GetTitle()
		if item.GetWeight() == 0 {
			text += fmt.Sprintf("%s ~~%s~~\n", icon, title)
		} else {
			text += fmt.Sprintf("%s **%s**\n", icon, title)
		}
		if item.GetCriterionName() != "" {
			text += fmt.Sprintf("   _%s_\n", item.GetCriterionName())
		}
	}
	return text
}

func recTypeIconMax(t string) string {
	switch t {
	case "reminder":
		return "🔔"
	case "alarm":
		return "🚨"
	case "expiration_reminder":
		return "⏰"
	default:
		return "💡"
	}
}

// SendNotification sends a bot notification to a chat via MAX.
func (h *Handler) SendNotification(chatID int64, title, body string) {
	text := BuildNotificationMessage(title, body)
	if err := h.client.SendMessage(chatID, text, nil); err != nil {
		obs.BG("max").Error(err, "send notification to chat", "chat_id", chatID)
	}
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

// unused – kept for future use
var _ = strconv.FormatInt
