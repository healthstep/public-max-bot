package bot

import (
	"context"
	"strconv"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/porebric/logger"
)

func (h *Handler) handleProgress(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	h.clearMaxLabUpload(maxUserID)
	h.clearAnalysisPickMax(maxUserID)
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

	groupsResp, err := h.healthClient.ListGroups(ctx, &healthpb.ListGroupsRequest{})
	if err != nil {
		logger.Error(ctx, err, "max list groups for progress", "user_id", userID)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить группы. Попробуйте позже.", nil)
		return
	}

	text := formatProgressGroupedMarkdown(prog, criteria.GetEntries(), groupsResp.GetGroups())

	_ = h.client.SendMessage(chatID, text, progressKeyboardMax())
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
