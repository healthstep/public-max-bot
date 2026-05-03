package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/porebric/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var pendingAnalysisPickMax sync.Map

func (h *Handler) clearAnalysisPickMax(maxUserID string) {
	pendingAnalysisPickMax.Delete(maxUserID)
}

func progressKeyboardMax() *InlineKeyboard {
	return &InlineKeyboard{
		Buttons: [][]Button{
			{{Type: "callback", Text: "📋 Как получить", Payload: "menu:how_to_get"}},
			{{Type: "callback", Text: "➕ Добавить данные", Payload: "menu:add_data"}},
			{{Type: "callback", Text: "◀️ Назад", Payload: "menu:back"}},
		},
	}
}

func (h *Handler) handleProgressHowToMax(ctx context.Context, chatID int64, maxUserID string) {
	resp, err := h.healthClient.ListAnalyses(ctx, &healthpb.ListAnalysesRequest{})
	if err != nil {
		logger.Error(ctx, err, "max list analyses")
		_ = h.client.SendMessage(chatID, "Не удалось загрузить список анализов. Попробуйте позже.", mainMenuKeyboard())
		return
	}
	list := resp.GetAnalyses()
	if len(list) == 0 {
		_ = h.client.SendMessage(chatID, "В системе пока нет ни одного анализа. Обратитесь к администратору.", mainMenuKeyboard())
		return
	}
	var b strings.Builder
	b.WriteString("**Введите число — id анализа**, по которому нужна инструкция (из списка ниже).\n\n")
	for _, a := range list {
		b.WriteString(fmt.Sprintf("%d — %s\n", a.GetId(), a.GetName()))
	}
	pendingAnalysisPickMax.Store(maxUserID, struct{}{})
	kb := &InlineKeyboard{Buttons: [][]Button{
		{{Type: "callback", Text: "◀️ Назад в меню", Payload: "menu:back"}},
	}}
	_ = h.client.SendMessage(chatID, b.String(), kb)
}

func (h *Handler) handleMaxAnalysisPickReply(ctx context.Context, msg *Message, maxUserID string) bool {
	if _, ok := pendingAnalysisPickMax.Load(maxUserID); !ok {
		return false
	}
	text := strings.TrimSpace(msg.Body.Text)
	if strings.HasPrefix(text, "/") {
		return false
	}
	pendingAnalysisPickMax.Delete(maxUserID)
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 {
		_ = h.client.SendMessage(msg.Recipient.ChatID, "Неверный номер. Возвращаем вас в главное меню.", nil)
		h.sendMainMenu(ctx, msg.Recipient.ChatID)
		return true
	}
	ar, err := h.healthClient.GetAnalysis(ctx, &healthpb.GetAnalysisRequest{Id: id})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.NotFound {
			_ = h.client.SendMessage(msg.Recipient.ChatID, "Такого id нет в списке. Возвращаем вас в главное меню.", nil)
			h.sendMainMenu(ctx, msg.Recipient.ChatID)
			return true
		}
		logger.Error(ctx, err, "max get analysis", "id", id)
		_ = h.client.SendMessage(msg.Recipient.ChatID, "Ошибка загрузки. Возвращаем вас в главное меню.", nil)
		h.sendMainMenu(ctx, msg.Recipient.ChatID)
		return true
	}
	a := ar.GetAnalysis()
	instr := strings.TrimSpace(a.GetInstruction())
	var body string
	if instr == "" {
		body = fmt.Sprintf("**%s**\n\nИнструкция пока не заполнена.", a.GetName())
	} else {
		body = fmt.Sprintf("**%s**\n\n%s", a.GetName(), instr)
	}
	_ = h.client.SendMessageFormatted(msg.Recipient.ChatID, body, "markdown", nil)
	h.sendMainMenu(ctx, msg.Recipient.ChatID)
	return true
}
