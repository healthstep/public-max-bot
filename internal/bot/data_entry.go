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

// handleAddData shows the flat list of available criteria.
func (h *Handler) handleAddData(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	userSex := h.getUserSex(ctx, maxUserID)

	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		UserId:  *chat.UserID,
		UserSex: userSex,
	})
	if err != nil {
		log.Printf("list criteria: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить список показателей. Попробуйте позже.", nil)
		return
	}

	if len(resp.GetCriteria()) == 0 {
		_ = h.client.SendMessage(chatID, "Нет доступных показателей.", nil)
		return
	}

	// Cache names and input types.
	for _, c := range resp.GetCriteria() {
		criterionNames.Store(c.GetId(), c.GetName())
		criterionInputTypes.Store(c.GetId(), c.GetInputType())
	}

	var rows [][]Button
	for _, c := range resp.GetCriteria() {
		icon := criterionLevelIcon(int(c.GetLevel()))
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    icon + " " + c.GetName(),
			Payload: "data:select:" + c.GetId(),
		}})
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}})

	prompt := "➕ **Добавить данные**\n\nВыберите показатель:\n\n_Отправьте «отмена» в любой момент, чтобы сбросить все ваши данные._"
	_ = h.client.SendMessage(chatID, prompt, &InlineKeyboard{Buttons: rows})
}

// handleDataCallback handles data:* callbacks.
func (h *Handler) handleDataCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 3)
	if len(parts) < 3 {
		return
	}
	subType := parts[1]
	value := parts[2]
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)

	switch subType {
	case "select":
		// value = criterionID
		name := ""
		if v, ok := criterionNames.Load(value); ok {
			name = v.(string)
		}
		inputType := "numeric"
		if v, ok := criterionInputTypes.Load(value); ok {
			inputType = v.(string)
		}

		pendingInput.Store(maxUserID, PendingInput{
			CriterionID:   value,
			CriterionName: name,
			InputType:     inputType,
		})

		var prompt string
		switch inputType {
		case "check":
			prompt = fmt.Sprintf(
				"Отправьте **+**, если у вас уже есть **%s**, и **-**, если нет.\n\n_Отправьте «отмена» чтобы сбросить все ваши данные._",
				name,
			)
		default:
			prompt = fmt.Sprintf(
				"Введите число для показателя **%s**:\n\n_Отправьте «отмена» чтобы сбросить все ваши данные._",
				name,
			)
		}
		_ = h.client.SendMessage(chatID, prompt, &InlineKeyboard{Buttons: [][]Button{
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		}})
	}
}

// handleCancelAll resets all user criteria.
func (h *Handler) handleCancelAll(ctx context.Context, msg *Message) {
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(msg.Recipient.ChatID, "Вы не авторизованы.", nil)
		return
	}
	_, err = h.healthClient.ResetCriteria(ctx, &healthpb.ResetCriteriaRequest{
		UserId: *chat.UserID,
	})
	if err != nil {
		log.Printf("reset criteria: %v", err)
		_ = h.client.SendMessage(msg.Recipient.ChatID, "Не удалось сбросить данные. Попробуйте позже.", nil)
		return
	}
	_ = h.client.SendMessage(msg.Recipient.ChatID, "✅ Все ваши данные сброшены.", nil)
	h.sendMainMenu(ctx, msg.Recipient.ChatID)
}

// handleInputCallback handles "input:*" callbacks (legacy, kept for compatibility).
func (h *Handler) handleInputCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 4)
	if len(parts) < 4 {
		return
	}
	criterionID := parts[2]
	value := parts[3]
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	h.saveCriterionValue(ctx, chatID, maxUserID, criterionID, value)
}

func (h *Handler) handleNumericInput(ctx context.Context, msg *Message, pending PendingInput) {
	chatID := msg.Recipient.ChatID
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	text := strings.TrimSpace(msg.Body.Text)

	var value string
	switch pending.InputType {
	case "check":
		switch text {
		case "+":
			value = "1"
		case "-":
			_ = h.client.SendMessage(chatID, fmt.Sprintf("Понято — **%s** отмечен как отсутствующий.", pending.CriterionName), nil)
			h.sendMainMenu(ctx, chatID)
			return
		default:
			_ = h.client.SendMessage(chatID, "Пожалуйста, отправьте **+** или **-**.", nil)
			pendingInput.Store(maxUserID, pending)
			return
		}
	default:
		numVal, err := strconv.ParseFloat(text, 64)
		if err != nil {
			_ = h.client.SendMessage(chatID, "Пожалуйста, введите корректное число.", nil)
			pendingInput.Store(maxUserID, pending)
			return
		}
		value = fmt.Sprintf("%.2f", numVal)
	}

	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	h.saveCriterionValue(ctx, chatID, maxUserID, pending.CriterionID, value)
	_ = h.client.SendMessage(chatID, fmt.Sprintf("✅ **%s** сохранено!", pending.CriterionName), nil)
	h.sendMainMenu(ctx, chatID)
}

func (h *Handler) saveCriterionValue(ctx context.Context, chatID int64, maxUserID, criterionID, value string) {
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
	}
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
