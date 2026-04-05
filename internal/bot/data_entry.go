package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
)

// getUserSex fetches user sex from core-users.
func (h *Handler) getUserSex(ctx context.Context, maxUserID string) string {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		return ""
	}
	resp, err := h.usersClient.GetUser(ctx, &userspb.GetUserRequest{UserId: *chat.UserID})
	if err != nil {
		return ""
	}
	return resp.GetSex()
}

// handleAddData is Step 1: show list of analyses.
func (h *Handler) handleAddData(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	userSex := h.getUserSex(ctx, maxUserID)

	resp, err := h.healthClient.ListAnalysis(ctx, &healthpb.ListAnalysisRequest{
		UserId:  *chat.UserID,
		UserSex: userSex,
	})
	if err != nil {
		log.Printf("list analysis: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить список анализов. Попробуйте позже.", nil)
		return
	}

	var rows [][]Button
	for _, a := range resp.GetAnalyses() {
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    "🔬 " + a.GetName(),
			Payload: "data:analysis:" + a.GetId(),
		}})
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}})

	_ = h.client.SendMessage(chatID, "➕ **Добавить данные**\n\nВыберите анализ:", &InlineKeyboard{Buttons: rows})
}

// handleDataCallback handles data:* callbacks.
func (h *Handler) handleDataCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 3)
	if len(parts) < 3 {
		return
	}
	subType := parts[1]
	value := parts[2]

	switch subType {
	case "analysis":
		maxUserID := strconv.FormatInt(cb.User.UserID, 10)
		pendingAnalysis.Store(maxUserID, value)
		h.showCriteriaForAnalysis(ctx, cb, chatID, value)
	case "manual":
		// value = criterionID  (name looked up from criterionNames map)
		maxUserID := strconv.FormatInt(cb.User.UserID, 10)
		name := ""
		if v, ok := criterionNames.Load(value); ok {
			name = v.(string)
		}
		analysisID := ""
		if v, ok := pendingAnalysis.Load(maxUserID); ok {
			analysisID = v.(string)
		}
		pendingInput.Store(maxUserID, PendingInput{
			CriterionID:   value,
			CriterionName: name,
			AnalysisID:    analysisID,
		})
		_ = h.client.SendMessage(chatID,
			fmt.Sprintf("Введите числовое значение для **%s**:\n\n_Отправьте «отмена» чтобы сбросить все данные этого анализа._", name),
			&InlineKeyboard{Buttons: [][]Button{
				{{Type: "callback", Text: "❌ Отмена", Payload: "menu:back"}},
			}},
		)
	case "done":
		// value = criterionID
		maxUserID := strconv.FormatInt(cb.User.UserID, 10)
		h.createMarkDoneEvent(ctx, chatID, maxUserID, value)
	case "upload":
		// value = criterionID
		_ = h.client.SendMessage(chatID,
			fmt.Sprintf("Загрузите файл (PDF, фото анализов) в этот чат.\n\nID критерия: `%s`\n\n_Отправьте «отмена» чтобы сбросить все данные анализа._", value),
			&InlineKeyboard{Buttons: [][]Button{
				{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back_analysis"}},
			}},
		)
	}
}

// showCriteriaForAnalysis is Step 2: show criteria for the selected analysis.
func (h *Handler) showCriteriaForAnalysis(ctx context.Context, cb *Callback, chatID int64, analysisID string) {
	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		AnalysisId: analysisID,
	})
	if err != nil {
		log.Printf("list criteria for analysis %s: %v", analysisID, err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить показатели. Попробуйте позже.", nil)
		return
	}

	if len(resp.GetCriteria()) == 0 {
		_ = h.client.SendMessage(chatID, "В этом анализе нет показателей.", nil)
		return
	}

	var rows [][]Button
	for _, c := range resp.GetCriteria() {
		// Cache name so we don't embed it in callback payload.
		criterionNames.Store(c.GetId(), c.GetName())

		levelIcon := criterionLevelIcon(int(c.GetLevel()))
		label := levelIcon + " " + c.GetName()
		rows = append(rows,
			[]Button{
				{Type: "callback", Text: label + " — ввести", Payload: "data:manual:" + c.GetId()},
				{Type: "callback", Text: "✅ выполнено", Payload: "data:done:" + c.GetId()},
				{Type: "callback", Text: "📎 файл", Payload: "data:upload:" + c.GetId()},
			},
		)
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ К анализам", Payload: "menu:back_analysis"}})

	prompt := "Выберите показатель и способ ввода:\n\n_Отправьте «отмена» в любой момент, чтобы сбросить все данные этого анализа._"
	_ = h.client.SendMessage(chatID, prompt, &InlineKeyboard{Buttons: rows})
}

// handleCancelAnalysis resets all user criteria for an analysis.
func (h *Handler) handleCancelAnalysis(ctx context.Context, msg *Message, analysisID string) {
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(msg.Recipient.ChatID, "Вы не авторизованы.", nil)
		return
	}
	_, err = h.healthClient.ResetAnalysisCriteria(ctx, &healthpb.ResetAnalysisCriteriaRequest{
		UserId:     *chat.UserID,
		AnalysisId: analysisID,
	})
	if err != nil {
		log.Printf("reset analysis criteria: %v", err)
		_ = h.client.SendMessage(msg.Recipient.ChatID, "Не удалось сбросить данные. Попробуйте позже.", nil)
		return
	}
	_ = h.client.SendMessage(msg.Recipient.ChatID, "✅ Все данные этого анализа сброшены.", nil)
	h.sendMainMenu(ctx, msg.Recipient.ChatID)
}

// handleInputCallback handles "input:*" callbacks (legacy, kept for compatibility).
func (h *Handler) handleInputCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 4)
	if len(parts) < 4 {
		return
	}
	// inputType := parts[1]  // always "boolean" in old flow
	criterionID := parts[2]
	value := parts[3]
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	h.createCriterionValue(ctx, chatID, maxUserID, criterionID, value)
}

func (h *Handler) handleNumericInput(ctx context.Context, msg *Message, pending PendingInput) {
	chatID := msg.Recipient.ChatID
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)

	numVal, err := strconv.ParseFloat(strings.TrimSpace(msg.Body.Text), 64)
	if err != nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, введите корректное число.", nil)
		pendingInput.Store(maxUserID, pending)
		return
	}

	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	_, err = h.healthClient.SetUserCriterion(ctx, &healthpb.SetUserCriterionRequest{
		UserId:      *chat.UserID,
		CriterionId: pending.CriterionID,
		Value:       fmt.Sprintf("%.2f", numVal),
		Source:      "max_bot",
	})
	if err != nil {
		log.Printf("set user criterion: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось сохранить значение. Попробуйте позже.", nil)
		return
	}

	_ = h.client.SendMessage(chatID, fmt.Sprintf("✅ **%s**: %.2f — сохранено!", pending.CriterionName, numVal), nil)
	h.sendMainMenu(ctx, chatID)
}

func (h *Handler) createCriterionValue(ctx context.Context, chatID int64, maxUserID, criterionID, value string) {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	_, err = h.healthClient.SetUserCriterion(ctx, &healthpb.SetUserCriterionRequest{
		UserId:      *chat.UserID,
		CriterionId: criterionID,
		Value:       value,
		Source:      "max_bot",
	})
	if err != nil {
		log.Printf("set user criterion: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось сохранить значение. Попробуйте позже.", nil)
		return
	}

	_ = h.client.SendMessage(chatID, "✅ Значение сохранено!", nil)
	h.sendMainMenu(ctx, chatID)
}

func (h *Handler) createMarkDoneEvent(ctx context.Context, chatID int64, maxUserID, criterionID string) {
	h.createCriterionValue(ctx, chatID, maxUserID, criterionID, "1")
}

func criterionLevelIcon(level int) string {
	switch level {
	case 1:
		return "⭐"
	case 2:
		return "⭐⭐"
	case 3:
		return "⭐⭐⭐"
	default:
		return "•"
	}
}
