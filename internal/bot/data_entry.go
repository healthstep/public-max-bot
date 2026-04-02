package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handler) handleAddData(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{})
	if err != nil {
		log.Printf("list criteria: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить показатели. Попробуйте позже.", nil)
		return
	}

	var rows [][]Button
	for _, c := range resp.GetCriteria() {
		if !c.GetIsActive() {
			continue
		}
		payload := fmt.Sprintf("data:%s:%s", c.GetValueType(), c.GetId())
		rows = append(rows, []Button{{Type: "callback", Text: c.GetName(), Payload: payload}})
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}})

	_ = h.client.SendMessage(chatID, "➕ **Добавить данные**\n\nВыберите показатель:", &InlineKeyboard{Buttons: rows})
}

// handleDataCallback handles "data:<value_type>:<criterion_id>" callbacks.
func (h *Handler) handleDataCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 3)
	if len(parts) < 3 {
		return
	}
	valueType := parts[1]
	criterionID := parts[2]

	maxUserID := strconv.FormatInt(cb.User.UserID, 10)

	switch valueType {
	case "numeric":
		pendingInput.Store(maxUserID, PendingInput{
			CriterionID:   criterionID,
			CriterionName: valueType,
		})
		_ = h.client.SendMessage(chatID,
			"Введите числовое значение показателя:",
			&InlineKeyboard{
				Buttons: [][]Button{
					{{Type: "callback", Text: "❌ Отмена", Payload: "menu:back"}},
				},
			},
		)

	case "boolean":
		_ = h.client.SendMessage(chatID,
			"Выберите значение:",
			&InlineKeyboard{
				Buttons: [][]Button{
					{
						{Type: "callback", Text: "✅ Да", Payload: fmt.Sprintf("input:boolean:%s:true", criterionID)},
						{Type: "callback", Text: "❌ Нет", Payload: fmt.Sprintf("input:boolean:%s:false", criterionID)},
					},
					{{Type: "callback", Text: "◀️ Назад", Payload: "menu:add_data"}},
				},
			},
		)

	case "mark_done":
		h.createMarkDoneEvent(ctx, chatID, maxUserID, criterionID)
	}
}

// handleInputCallback handles "input:boolean:<id>:<value>" callbacks.
func (h *Handler) handleInputCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 4)
	if len(parts) < 4 {
		return
	}
	inputType := parts[1]
	criterionID := parts[2]
	value := parts[3]

	maxUserID := strconv.FormatInt(cb.User.UserID, 10)

	switch inputType {
	case "boolean":
		h.createBooleanEvent(ctx, chatID, maxUserID, criterionID, value)
	}
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

	_, err = h.healthClient.CreateNumericEvent(ctx, &healthpb.CreateNumericEventRequest{
		UserId:            *chat.UserID,
		HealthCriterionId: pending.CriterionID,
		NumericValue:      numVal,
		OccurredAt:        timestamppb.Now(),
		Source:            "max_bot",
	})
	if err != nil {
		log.Printf("create numeric event: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось сохранить значение. Попробуйте позже.", nil)
		return
	}

	_ = h.client.SendMessage(chatID, fmt.Sprintf("✅ Значение **%.1f** сохранено!", numVal), nil)
	h.sendMainMenu(ctx, chatID)
}

func (h *Handler) createBooleanEvent(ctx context.Context, chatID int64, maxUserID, criterionID, value string) {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	_, err = h.healthClient.CreateBooleanEvent(ctx, &healthpb.CreateBooleanEventRequest{
		UserId:            *chat.UserID,
		HealthCriterionId: criterionID,
		BooleanValue:      value,
		OccurredAt:        timestamppb.Now(),
		Source:            "max_bot",
	})
	if err != nil {
		log.Printf("create boolean event: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось сохранить значение. Попробуйте позже.", nil)
		return
	}

	label := "Да"
	if value == "false" {
		label = "Нет"
	}
	_ = h.client.SendMessage(chatID, fmt.Sprintf("✅ Значение «%s» сохранено!", label), nil)
	h.sendMainMenu(ctx, chatID)
}

func (h *Handler) createMarkDoneEvent(ctx context.Context, chatID int64, maxUserID, criterionID string) {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	_, err = h.healthClient.CreateMarkDoneEvent(ctx, &healthpb.CreateMarkDoneEventRequest{
		UserId:            *chat.UserID,
		HealthCriterionId: criterionID,
		OccurredAt:        timestamppb.Now(),
		Source:            "max_bot",
	})
	if err != nil {
		log.Printf("create mark-done event: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось отметить выполнение. Попробуйте позже.", nil)
		return
	}

	_ = h.client.SendMessage(chatID, "✅ Отмечено как выполнено!", nil)
	h.sendMainMenu(ctx, chatID)
}
