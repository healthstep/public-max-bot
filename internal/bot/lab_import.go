package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/porebric/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxLabFiles = 5
const maxLabDebounce = 2 * time.Second

var maxLabConfirmPendingID sync.Map
var pendingMaxLabImport sync.Map

// maxLabBatches: дебаунс PDF (до 5) без пункта меню
var maxLabBatches sync.Map

type maxLabBatchState struct {
	mu     sync.Mutex
	files  []rawMaxLabFile
	chatID int64
	timer  *time.Timer
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
	maxLabConfirmPendingID.Delete(maxUserID)
	cancelMaxLabBatch(maxUserID)
}

func cancelMaxLabBatch(maxUserID string) {
	v, ok := maxLabBatches.Load(maxUserID)
	if !ok {
		return
	}
	st := v.(*maxLabBatchState)
	st.mu.Lock()
	if st.timer != nil {
		st.timer.Stop()
	}
	st.mu.Unlock()
	maxLabBatches.Delete(maxUserID)
}

// tryHandleMaxLabMessage: любой PDF-вложение(я) в сообщении — очередь и debounce → ImportCriteriaFromPdf.
func (h *Handler) tryHandleMaxLabMessage(ctx context.Context, msg *Message) bool {
	maxUserID := fmtD(msg.Sender.UserID)
	chatID := msg.Recipient.ChatID

	if len(msg.Body.Attachments) == 0 {
		return false
	}

	chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		var atts []struct{ Type string `json:"type"` }
		_ = json.Unmarshal(msg.Body.Attachments, &atts)
		for _, a := range atts {
			if a.Type == "file" {
				_ = h.client.SendMessage(chatID, "Сначала зарегистрируйтесь с помощью /start", nil)
				return true
			}
		}
		return false
	}

	fbs := h.extractAndDownloadMaxPdfs(ctx, msg)
	if len(fbs) == 0 {
		var atts []struct{ Type string `json:"type"` }
		_ = json.Unmarshal(msg.Body.Attachments, &atts)
		for _, a := range atts {
			if a.Type == "file" {
				_ = h.client.SendMessage(chatID, "Нужен файл **PDF** (анализ).", nil)
				return true
			}
		}
		return false
	}

	if _, busy := pendingMaxLabImport.Load(maxUserID); busy {
		_ = h.client.SendMessage(chatID, "Дождитесь окончания предыдущей обработки.", nil)
		return true
	}

	v, _ := maxLabBatches.LoadOrStore(maxUserID, &maxLabBatchState{})
	st := v.(*maxLabBatchState)
	st.mu.Lock()
	if st.chatID == 0 {
		st.chatID = chatID
	}
	space := maxLabFiles - len(st.files)
	if space <= 0 {
		st.mu.Unlock()
		_ = h.client.SendMessage(chatID, fmt.Sprintf("Максимум **%d** PDF в одной пачке.", maxLabFiles), nil)
		return true
	}
	add := fbs
	if len(add) > space {
		add = add[:space]
	}
	wasEmpty := len(st.files) == 0
	for _, f := range add {
		st.files = append(st.files, f)
	}
	if st.timer != nil {
		st.timer.Stop()
	}
	uid := maxUserID
	cid := st.chatID
	if wasEmpty {
		_ = h.client.SendMessage(chatID,
			"📄 **PDF получен.** Можно за "+fmt.Sprintf("%d", int(maxLabDebounce.Seconds()))+" с прислать ещё (до 5 вместе).", nil)
	}
	st.timer = time.AfterFunc(maxLabDebounce, func() {
		h.flushMaxLabBatch(context.Background(), uid, cid)
	})
	st.mu.Unlock()
	return true
}

func (h *Handler) flushMaxLabBatch(ctx context.Context, maxUserID string, chatID int64) {
	v, ok := maxLabBatches.Load(maxUserID)
	if !ok {
		return
	}
	st := v.(*maxLabBatchState)
	st.mu.Lock()
	if len(st.files) == 0 {
		st.mu.Unlock()
		return
	}
	files := make([]rawMaxLabFile, len(st.files))
	copy(files, st.files)
	st.files = nil
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	st.mu.Unlock()
	maxLabBatches.Delete(maxUserID)

	if !tryStartMaxLabImport(maxUserID) {
		_ = h.client.SendMessage(chatID, "Подождите, идёт обработка предыдущей загрузки…", nil)
		return
	}
	_ = h.client.SendMessage(chatID, "Подождите, идёт обработка файла…", nil)
	go h.runMaxLabImport(ctx, chatID, maxUserID, files)
}

func tryStartMaxLabImport(maxUserID string) bool {
	_, loaded := pendingMaxLabImport.LoadOrStore(maxUserID, struct{}{})
	return !loaded
}

func doneMaxLabImport(maxUserID string) { pendingMaxLabImport.Delete(maxUserID) }

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

func escapeMaxMarkdown(s string) string {
	s = strings.ReplaceAll(s, "*", "·")
	return s
}

func (h *Handler) handleMaxLabConfirm(ctx context.Context, chatID int64, maxUserID string, accept bool) {
	pidVal, ok := maxLabConfirmPendingID.Load(maxUserID)
	if !ok {
		_ = h.client.SendMessage(chatID, "Нет данных для подтверждения. Сначала пришлите **PDF** с анализом в этот чат.", nil)
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
	case "lab:yes":
		h.handleMaxLabConfirm(ctx, chatID, maxUserID, true)
	case "lab:no":
		h.handleMaxLabConfirm(ctx, chatID, maxUserID, false)
	}
}
