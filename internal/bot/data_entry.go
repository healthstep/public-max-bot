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

// handleAddData shows the list of criteria groups.
func (h *Handler) handleAddData(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, сначала зарегистрируйтесь с помощью /start", nil)
		return
	}

	userSex := h.getUserSex(ctx, maxUserID)

	// Fetch groups.
	groupResp, _ := h.healthClient.ListGroups(ctx, &healthpb.ListGroupsRequest{})

	// Fetch criteria.
	criteriaResp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		UserId:  *chat.UserID,
		UserSex: userSex,
	})
	if err != nil {
		log.Printf("list criteria: %v", err)
		_ = h.client.SendMessage(chatID, "Не удалось загрузить список показателей. Попробуйте позже.", nil)
		return
	}

	// Get user values for ✅ markers.
	filledMap := make(map[string]bool)
	ucResp, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{
		UserId: *chat.UserID, UserSex: userSex,
	})
	if err == nil {
		for _, e := range ucResp.GetEntries() {
			if e.GetValue() != "" {
				filledMap[e.GetCriterionId()] = true
			}
		}
	}

	// Cache names and input types.
	for _, c := range criteriaResp.GetCriteria() {
		criterionNames.Store(c.GetId(), c.GetName())
		criterionInputTypes.Store(c.GetId(), c.GetInputType())
	}

	// Group criteria by group_id.
	byGroup := make(map[string][]*healthpb.Criterion)
	ungrouped := []*healthpb.Criterion{}
	for _, c := range criteriaResp.GetCriteria() {
		gid := c.GetGroupId()
		if gid == "" {
			ungrouped = append(ungrouped, c)
		} else {
			byGroup[gid] = append(byGroup[gid], c)
		}
	}

	groups := groupResp.GetGroups()
	if len(groups) == 0 {
		h.showFlatCriteriaList(chatID, criteriaResp.GetCriteria(), filledMap)
		return
	}

	var rows [][]Button
	for _, g := range groups {
		items := byGroup[g.GetId()]
		if len(items) == 0 {
			continue
		}
		total := len(items)
		filled := 0
		for _, c := range items {
			if filledMap[c.GetId()] {
				filled++
			}
		}
		label := fmt.Sprintf("%s (%d/%d)", g.GetName(), filled, total)
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    label,
			Payload: "data:group:" + g.GetId(),
		}})
		criterionGroups.Store(g.GetId(), items)
	}
	if len(ungrouped) > 0 {
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    "Другое",
			Payload: "data:group:__ungrouped",
		}})
		criterionGroups.Store("__ungrouped", ungrouped)
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}})

	prompt := "➕ **Добавить данные**\n\nВыберите группу показателей:\n\n_Отправьте «отмена» в любой момент, чтобы сбросить все ваши данные._"
	_ = h.client.SendMessage(chatID, prompt, &InlineKeyboard{Buttons: rows})
}

// handleGroupCallback shows criteria within a selected group.
func (h *Handler) handleGroupCallback(ctx context.Context, cb *Callback, chatID int64, groupID string) {
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)

	val, ok := criterionGroups.Load(groupID)
	if !ok {
		_ = h.client.SendMessage(chatID, "Группа не найдена.", nil)
		return
	}
	criteria := val.([]*healthpb.Criterion)

	// Get user values.
	chat, _ := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	filledMap := make(map[string]bool)
	if chat.UserID != nil {
		userSex := h.getUserSex(ctx, maxUserID)
		ucResp, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{
			UserId: *chat.UserID, UserSex: userSex,
		})
		if err == nil {
			for _, e := range ucResp.GetEntries() {
				if e.GetValue() != "" {
					filledMap[e.GetCriterionId()] = true
				}
			}
		}
	}

	var rows [][]Button
	for _, c := range criteria {
		label := c.GetName()
		if filledMap[c.GetId()] {
			label = "✅ " + label
		}
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    label,
			Payload: "data:select:" + c.GetId(),
		}})
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ Назад", Payload: "data:groups:back"}})
	_ = h.client.SendMessage(chatID, "Выберите показатель:", &InlineKeyboard{Buttons: rows})
}

// showFlatCriteriaList renders a flat (ungrouped) criteria list.
func (h *Handler) showFlatCriteriaList(chatID int64, criteria []*healthpb.Criterion, filledMap map[string]bool) {
	if len(criteria) == 0 {
		_ = h.client.SendMessage(chatID, "Нет доступных показателей.", nil)
		return
	}
	var rows [][]Button
	for _, c := range criteria {
		label := c.GetName()
		if filledMap[c.GetId()] {
			label = "✅ " + label
		}
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    label,
			Payload: "data:select:" + c.GetId(),
		}})
	}
	rows = append(rows, []Button{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}})
	_ = h.client.SendMessage(chatID, "Выберите показатель:", &InlineKeyboard{Buttons: rows})
}

// handleDataCallback handles data:* callbacks.
func (h *Handler) handleDataCallback(ctx context.Context, cb *Callback, chatID int64) {
	parts := strings.SplitN(cb.Payload, ":", 3)
	if len(parts) < 2 {
		return
	}
	subType := parts[1]
	value := ""
	if len(parts) > 2 {
		value = parts[2]
	}
	maxUserID := strconv.FormatInt(cb.User.UserID, 10)

	switch subType {
	case "group":
		h.handleGroupCallback(ctx, cb, chatID, value)
	case "groups":
		// "data:groups:back" → go back to groups list
		h.handleAddData(ctx, cb, chatID)
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
		case "boolean":
			prompt = fmt.Sprintf(
				"Отправьте **+**, если результат **%s** положительный, и **-**, если отрицательный.\n\n_Отправьте «отмена» чтобы сбросить все ваши данные._",
				name,
			)
		default:
			prompt = fmt.Sprintf(
				"Введите число для показателя **%s**:\n\n_Отправьте «отмена» чтобы сбросить все ваши данные._",
				name,
			)
		}
		_ = h.client.SendMessage(chatID, prompt, &InlineKeyboard{Buttons: [][]Button{
			{{Type: "callback", Text: "◀️ Назад", Payload: "data:groups:back"}},
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
	case "boolean":
		switch text {
		case "+":
			value = "1"
		case "-":
			value = "0"
		default:
			_ = h.client.SendMessage(chatID, "Пожалуйста, отправьте **+** (положительный) или **-** (отрицательный).", nil)
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
