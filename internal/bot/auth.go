package bot

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"

	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-max-bot/internal/model"
)

func (h *Handler) handleStartWithKey(ctx context.Context, msg *Message, key string) {
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	chatID := msg.Recipient.ChatID

	chat, _ := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if chat != nil && chat.UserID != nil {
		_ = h.client.SendMessage(chatID, "Вы уже зарегистрированы! 🎉", nil)
		h.sendMainMenu(ctx, chatID)
		return
	}

	resolveResp, err := h.usersClient.ResolveAuthKey(ctx, &userspb.ResolveAuthKeyRequest{Key: key})
	if err != nil || !resolveResp.GetValid() {
		_ = h.client.SendMessage(chatID, "Ссылка недействительна или устарела. Попробуйте получить новую в приложении.", nil)
		return
	}

	provisionalUserID := resolveResp.GetProvisionalUserId()
	if provisionalUserID == "" {
		createResp, err := h.usersClient.CreateProvisionalUser(ctx, &userspb.CreateProvisionalUserRequest{})
		if err != nil {
			log.Printf("create provisional user: %v", err)
			_ = h.client.SendMessage(chatID, "Произошла ошибка. Попробуйте позже.", nil)
			return
		}
		provisionalUserID = createResp.GetId()
	}

	if chat == nil {
		chat = &model.Chat{MaxUserID: maxUserID}
	}
	chatIDStr := strconv.FormatInt(chatID, 10)
	chat.ChatID = &chatIDStr
	chat.ProvisionalUserID = &provisionalUserID
	chat.AuthKey = &key
	if msg.Sender.Username != "" {
		chat.Username = &msg.Sender.Username
	}
	if err := h.chatRepo.Upsert(ctx, chat); err != nil {
		log.Printf("upsert chat: %v", err)
	}

	kb := &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "request_contact", Text: "📱 Поделиться номером"}},
		},
	}
	_ = h.client.SendMessage(chatID,
		"Добро пожаловать в **ЗдравоШаг**! 👋\n\nДля завершения регистрации, пожалуйста, поделитесь вашим номером телефона:",
		kb,
	)
}

func (h *Handler) handleStartWithoutKey(ctx context.Context, msg *Message) {
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	chatID := msg.Recipient.ChatID

	chat, _ := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if chat != nil && chat.UserID != nil {
		_ = h.client.SendMessage(chatID, "С возвращением! 👋", nil)
		h.sendMainMenu(ctx, chatID)
		return
	}

	createResp, err := h.usersClient.CreateProvisionalUser(ctx, &userspb.CreateProvisionalUserRequest{})
	if err != nil {
		log.Printf("create provisional user (no key): %v", err)
		_ = h.client.SendMessage(chatID, "Произошла ошибка. Попробуйте позже.", nil)
		return
	}
	provisionalUserID := createResp.GetId()

	if chat == nil {
		chat = &model.Chat{MaxUserID: maxUserID}
	}
	chatIDStr := strconv.FormatInt(chatID, 10)
	chat.ChatID = &chatIDStr
	chat.ProvisionalUserID = &provisionalUserID
	if msg.Sender.Username != "" {
		chat.Username = &msg.Sender.Username
	}
	if err := h.chatRepo.Upsert(ctx, chat); err != nil {
		log.Printf("upsert chat: %v", err)
	}

	kb := &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "request_contact", Text: "📱 Поделиться номером"}},
		},
	}
	_ = h.client.SendMessage(chatID,
		"Добро пожаловать в **ЗдравоШаг**! 👋\n\n"+
			"Я помогу вам следить за здоровьем: добавлять показатели, "+
			"отслеживать прогресс и не забывать о важных обследованиях.\n\n"+
			"Для начала, поделитесь вашим номером телефона:",
		kb,
	)
}

func (h *Handler) handleContact(ctx context.Context, msg *Message, phone string) {
	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	chatID := msg.Recipient.ChatID

	chat, _ := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if chat == nil || chat.ProvisionalUserID == nil {
		_ = h.client.SendMessage(chatID, "Пожалуйста, начните с команды /start", nil)
		return
	}

	authKey := ""
	if chat.AuthKey != nil {
		authKey = *chat.AuthKey
	}

	verifyResp, err := h.usersClient.VerifyPhone(ctx, &userspb.VerifyPhoneRequest{
		PhoneE164:         phone,
		ProvisionalUserId: *chat.ProvisionalUserID,
		AuthKey:           authKey,
		Platform:          "max",
	})
	if err != nil {
		log.Printf("verify phone for %s: %v", maxUserID, err)
		_ = h.client.SendMessage(chatID, "Не удалось подтвердить номер. Попробуйте ещё раз.", nil)
		return
	}

	userID := verifyResp.GetUserId()
	chat.UserID = &userID
	chat.ProvisionalUserID = nil
	chat.AuthKey = nil
	if err := h.chatRepo.Upsert(ctx, chat); err != nil {
		log.Printf("upsert chat after verify: %v", err)
	}

	// Publish auth token for the browser session if there was a key.
	if authKey != "" && h.nc != nil {
		tokenMsg, _ := json.Marshal(map[string]string{
			"key":     authKey,
			"token":   verifyResp.GetToken(),
			"user_id": userID,
		})
		_ = h.nc.Publish("auth.token."+authKey, tokenMsg)
	}

	welcomeMsg := "Регистрация завершена! ✅\n\nДобро пожаловать в **ЗдравоШаг**!"
	if h.siteURL != "" && verifyResp.GetToken() != "" {
		loginURL := h.siteURL + "/auth?token=" + verifyResp.GetToken()
		welcomeMsg += "\n\n🌐 [Войти на сайт одним нажатием](" + loginURL + ")"
	}
	_ = h.client.SendMessage(chatID, welcomeMsg, nil)
	h.sendOnboarding(ctx, chatID)
}

// extractPhone tries to pull a phone number from a contact attachment or text.
func extractPhone(msg *Message) string {
	if msg == nil {
		return ""
	}

	if len(msg.Body.Attachments) > 0 {
		var attachments []struct {
			Type    string `json:"type"`
			Payload struct {
				VcfInfo string `json:"vcf_info"`
				TamInfo *struct {
					Phone string `json:"phone"`
				} `json:"tam_info"`
			} `json:"payload"`
		}
		if json.Unmarshal(msg.Body.Attachments, &attachments) == nil {
			for _, a := range attachments {
				if a.Type != "contact" {
					continue
				}
				if a.Payload.TamInfo != nil && a.Payload.TamInfo.Phone != "" {
					return normalizePhone(a.Payload.TamInfo.Phone)
				}
				if a.Payload.VcfInfo != "" {
					for _, line := range strings.Split(a.Payload.VcfInfo, "\n") {
						if strings.HasPrefix(strings.ToUpper(line), "TEL") {
							parts := strings.SplitN(line, ":", 2)
							if len(parts) == 2 {
								return normalizePhone(strings.TrimSpace(parts[1]))
							}
						}
					}
				}
			}
		}
	}

	return ""
}

func normalizePhone(phone string) string {
	phone = strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '+' {
			return r
		}
		return -1
	}, phone)
	if strings.HasPrefix(phone, "8") && len(phone) == 11 {
		phone = "+7" + phone[1:]
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	return phone
}
