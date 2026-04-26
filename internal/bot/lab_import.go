package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/porebric/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxLabFiles = 5

// pendingMaxLab maps max user id string -> *maxLabCollectState
var pendingMaxLab sync.Map
var pendingMaxLabImport sync.Map
var maxLabConfirmPendingID sync.Map

type maxLabCollectState struct {
	ChatID int64
	Files  []rawMaxLabFile
}

type rawMaxLabFile struct {
	Name string
	Data []byte
}

type rawFilePayload struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	Filename string `json:"filename"`
}

func (h *Handler) clearMaxLabUpload(maxUserID string) {
	pendingMaxLab.Delete(maxUserID)
	maxLabConfirmPendingID.Delete(maxUserID)
}

func (h *Handler) startMaxLabCollection(ctx context.Context, chatID int64, maxUserID string) {
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Сначала зарегистрируйтесь с помощью /start", nil)
		return
	}
	h.clearMaxLabUpload(maxUserID)
	pendingMaxLab.Store(maxUserID, &maxLabCollectState{ChatID: chatID, Files: nil})
	text := "📄 **Загрузка анализов (PDF)**\n\n" +
		"Отправьте в чат до 5 PDF-файлов (по одному в сообщении или по очереди).\n\n" +
		"Когда закончите — нажмите **«Готово»** или напишите «готово»."
	kb := &InlineKeyboard{Buttons: [][]Button{
		{
			{Type: "callback", Text: "✅ Готово", Payload: "lab:done"},
			{Type: "callback", Text: "◀️ Отмена", Payload: "lab:cancel"},
		},
	}}
	if err := h.client.SendMessage(chatID, text, kb); err != nil {
		obs.BG("max").Error(err, "startMaxLab", "chat_id", chatID)
	}
}

// tryHandleMaxLabMessage returns true if the message was handled (lab flow or should not fall through to main menu).
func (h *Handler) tryHandleMaxLabMessage(ctx context.Context, msg *Message) bool {
	maxUserID := fmtD(msg.Sender.UserID)
	chatID := msg.Recipient.ChatID
	text := strings.TrimSpace(msg.Body.Text)

	// In collection mode
	if stVal, inLab := pendingMaxLab.Load(maxUserID); inLab {
		st := stVal.(*maxLabCollectState)
		// attachments: possible PDFs
		if fbs := h.extractAndDownloadMaxPdfs(ctx, msg); len(fbs) > 0 {
			if len(st.Files) >= maxLabFiles {
				_ = h.client.SendMessage(chatID, fmt.Sprintf("Уже %d/%d файлов. Нажмите «Готово».", len(st.Files), maxLabFiles), nil)
				return true
			}
			if _, busy := pendingMaxLabImport.Load(maxUserID); busy {
				_ = h.client.SendMessage(chatID, "Дождитесь окончания предыдущей обработки.", nil)
				return true
			}
			space := maxLabFiles - len(st.Files)
			if len(fbs) > space {
				fbs = fbs[:space]
			}
			for _, f := range fbs {
				st.Files = append(st.Files, f)
			}
			pendingMaxLab.Store(maxUserID, st)
			n := len(st.Files)
			_ = h.client.SendMessage(chatID, fmt.Sprintf("Принято файл(ов). Всего: %d/%d", n, maxLabFiles), nil)
			return true
		}
		if text != "" {
			if strings.HasPrefix(text, "/") {
				h.clearMaxLabUpload(maxUserID)
				return false
			}
			tl := strings.ToLower(text)
			if tl == "готово" || tl == "готов" {
				h.runMaxLabUploadDone(ctx, chatID, maxUserID)
				return true
			}
			_ = h.client.SendMessage(chatID, "Пришлите PDF-файл или нажмите «Готово» / «Отмена».", nil)
			return true
		}
		return true
	}

	// not in lab: user sent file without starting — hint
	if len(msg.Body.Attachments) > 0 {
		var atts []struct{ Type string `json:"type"` }
		_ = json.Unmarshal(msg.Body.Attachments, &atts)
		for _, a := range atts {
			if a.Type == "file" {
				_ = h.client.SendMessage(chatID, "Чтобы разобрать анализ, сначала нажмите в меню **«Загрузить анализы»**.", nil)
				return true
			}
		}
	}
	return false
}

func (h *Handler) extractAndDownloadMaxPdfs(ctx context.Context, msg *Message) []rawMaxLabFile {
	if len(msg.Body.Attachments) == 0 {
		return nil
	}
	var atts []struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Body.Attachments, &atts); err != nil {
		return nil
	}
	var out []rawMaxLabFile
	for _, a := range atts {
		if a.Type != "file" {
			continue
		}
		var p rawFilePayload
		if err := json.Unmarshal(a.Payload, &p); err != nil {
			continue
		}
		name := p.Filename
		if name == "" {
			name = "file.pdf"
		}
		if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			continue
		}
		data, err := h.client.DownloadUserFileInMax(ctx, p.URL, p.Token)
		if err != nil {
			logger.Error(ctx, err, "max download pdf from attachment", "filename", name)
			continue
		}
		if len(data) == 0 {
			continue
		}
		out = append(out, rawMaxLabFile{Name: name, Data: data})
	}
	return out
}

func fmtD(uid int64) string { return fmt.Sprintf("%d", uid) }

func (h *Handler) runMaxLabUploadDone(ctx context.Context, chatID int64, maxUserID string) {
	v, ok := pendingMaxLab.Load(maxUserID)
	if !ok {
		return
	}
	st := v.(*maxLabCollectState)
	if len(st.Files) == 0 {
		_ = h.client.SendMessage(chatID, "Вы ещё не отправили PDF. Пришлите файл(а) и нажмите «Готово».", nil)
		return
	}
	if !tryStartMaxLabImport(maxUserID) {
		_ = h.client.SendMessage(chatID, "Подождите, идёт обработка предыдущей загрузки…", nil)
		return
	}
	files := st.Files
	pendingMaxLab.Delete(maxUserID)
	_ = h.client.SendMessage(chatID, "Подождите, идёт обработка файла…", nil)
	go h.runMaxLabImport(ctx, chatID, maxUserID, files)
}

func tryStartMaxLabImport(maxUserID string) bool {
	_, loaded := pendingMaxLabImport.LoadOrStore(maxUserID, struct{}{})
	return !loaded
}

func doneMaxLabImport(maxUserID string) { pendingMaxLabImport.Delete(maxUserID) }

func (h *Handler) runMaxLabImport(ctx context.Context, chatID int64, maxUserID string, files []rawMaxLabFile) {
	defer doneMaxLabImport(maxUserID)
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		_ = h.client.SendMessage(chatID, "Ошибка: пользователь не найден.", nil)
		return
	}
	userID := *chat.UserID
	userSex := h.getUserSex(ctx, maxUserID)
	resp, err := h.maxCallImportCriteriaFromPdf(ctx, userID, userSex, files)
	if err != nil {
		logger.Error(ctx, err, "import lab max")
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
			_ = h.client.SendMessage(chatID, "Сервис разбора анализов временно недоступен. Попробуйте позже.", nil)
			return
		}
		_ = h.client.SendMessage(chatID, "Не удалось обработать файлы. Попробуйте ещё раз.", nil)
		return
	}
	pendingID := resp.GetPendingImportId()
	if pendingID == "" {
		rows := resp.GetUserCriteria()
		if len(rows) == 0 {
			_ = h.client.SendMessage(chatID, "Из PDF не удалось извлечь показатели.", nil)
			return
		}
	}
	text := formatMaxLabExtractionText(resp)
	maxLabConfirmPendingID.Store(maxUserID, pendingID)
	kb := &InlineKeyboard{Buttons: [][]Button{
		{
			{Type: "callback", Text: "✅ Сохранить в показатели", Payload: "lab:yes"},
			{Type: "callback", Text: "✖️ Отклонить", Payload: "lab:no"},
		},
	}}
	if err := h.client.SendMessage(chatID, text, kb); err != nil {
		obs.BG("max").Error(err, "max send lab result", "chat_id", chatID)
	}
}

func (h *Handler) maxCallImportCriteriaFromPdf(ctx context.Context, userID, userSex string, files []rawMaxLabFile) (*healthpb.ImportCriteriaFromPdfResponse, error) {
	stream, err := h.healthClient.ImportCriteriaFromPdf(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&healthpb.ImportCriteriaFromPdfRequest{UserId: userID, UserSex: userSex}); err != nil {
		_ = stream.CloseSend()
		return nil, err
	}
	const chunkSize = 64 * 1024
	for _, f := range files {
		if err := stream.Send(&healthpb.ImportCriteriaFromPdfRequest{Filename: f.Name}); err != nil {
			_ = stream.CloseSend()
			return nil, err
		}
		for i := 0; i < len(f.Data); {
			n := chunkSize
			if i+n > len(f.Data) {
				n = len(f.Data) - i
			}
			chunk := f.Data[i : i+n]
			if err := stream.Send(&healthpb.ImportCriteriaFromPdfRequest{Chunk: chunk}); err != nil {
				_ = stream.CloseSend()
				return nil, err
			}
			i += n
		}
	}
	return stream.CloseAndRecv()
}

func formatMaxLabExtractionText(resp *healthpb.ImportCriteriaFromPdfResponse) string {
	var b strings.Builder
	b.WriteString("📄 **Результат по файлу**\n\n")
	if note := strings.TrimSpace(resp.GetModelNote()); note != "" {
		b.WriteString("ℹ️ ")
		b.WriteString(escapeMaxMarkdown(note))
		b.WriteString("\n\n")
	}
	rows := resp.GetUserCriteria()
	if len(rows) == 0 {
		b.WriteString("Показатели не распознаны.")
		return b.String()
	}
	b.WriteString("Найдено:\n")
	for _, r := range rows {
		b.WriteString("• **" + escapeMaxMarkdown(r.GetCriterionName()) + ":** " + escapeMaxMarkdown(r.GetValue()) + "\n")
	}
	b.WriteString("\nСохранить в ваши показатели?")
	return b.String()
}

// escapeMaxMarkdown: minimal escaping for * in names (we use ** for bold in messages).
func escapeMaxMarkdown(s string) string {
	s = strings.ReplaceAll(s, "*", "·")
	return s
}

func (h *Handler) handleMaxLabConfirm(ctx context.Context, chatID int64, maxUserID string, accept bool) {
	pidVal, ok := maxLabConfirmPendingID.Load(maxUserID)
	if !ok {
		_ = h.client.SendMessage(chatID, "Нет данных для подтверждения. Сначала загрузите анализы через меню.", nil)
		return
	}
	pendingID, _ := pidVal.(string)
	if pendingID == "" {
		maxLabConfirmPendingID.Delete(maxUserID)
		_ = h.client.SendMessage(chatID, "Нечего применять.", nil)
		return
	}
	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		maxLabConfirmPendingID.Delete(maxUserID)
		_ = h.client.SendMessage(chatID, "Пользователь не найден.", nil)
		return
	}
	userID := *chat.UserID
	userSex := h.getUserSex(ctx, maxUserID)
	out, err := h.healthClient.ConfirmPendingImport(ctx, &healthpb.ConfirmPendingImportRequest{
		UserId:    userID,
		PendingId: pendingID,
		Accept:    accept,
		UserSex:   userSex,
	})
	maxLabConfirmPendingID.Delete(maxUserID)
	if err != nil {
		logger.Error(ctx, err, "max confirm lab import")
		_ = h.client.SendMessage(chatID, "Не удалось применить. Попробуйте в личном кабинете на сайте.", nil)
		h.sendMainMenu(ctx, chatID)
		return
	}
	if !out.GetSuccess() {
		msgE := out.GetErrorMessage()
		if msgE == "" {
			msgE = "ошибка"
		}
		_ = h.client.SendMessage(chatID, "Ошибка: "+msgE, nil)
		h.sendMainMenu(ctx, chatID)
		return
	}
	if accept {
		_ = h.client.SendMessage(chatID, fmt.Sprintf("Готово: в профиль записано обновлений: **%d**.", out.GetApplied()), nil)
	} else {
		_ = h.client.SendMessage(chatID, "Черновик отклонён, показатели на сайте не менялись.", nil)
	}
	h.sendMainMenu(ctx, chatID)
}

func (h *Handler) handleLabMaxCallbacks(ctx context.Context, cb *Callback, chatID int64) {
	maxUserID := fmtD(cb.User.UserID)
	switch cb.Payload {
	case "lab:done":
		h.runMaxLabUploadDone(ctx, chatID, maxUserID)
	case "lab:cancel":
		h.clearMaxLabUpload(maxUserID)
		_ = h.client.SendMessage(chatID, "Загрузка анализов отменена.", nil)
		h.sendMainMenu(ctx, chatID)
	case "lab:yes":
		h.handleMaxLabConfirm(ctx, chatID, maxUserID, true)
	case "lab:no":
		h.handleMaxLabConfirm(ctx, chatID, maxUserID, false)
	}
}