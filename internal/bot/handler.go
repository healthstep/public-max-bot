package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/helthtech/public-max-bot/internal/repository"
	"github.com/nats-io/nats.go"
	"github.com/porebric/logger"
	"github.com/porebric/resty/responses"
)

// pendingInput tracks users who are in the middle of typing a criterion value.
var pendingInput sync.Map

// criterionNames caches criterionID -> criterionName to avoid bloated callback payloads.
var criterionNames sync.Map

// criterionInputTypes caches criterionID -> inputType ("numeric", "check", "boolean").
var criterionInputTypes sync.Map

// criterionGroups caches groupID -> []*healthpb.Criterion for group-based navigation.
var criterionGroups sync.Map

type PendingInput struct {
	CriterionID   string
	CriterionName string
	InputType     string // "numeric" or "check"
}

// Handler processes MAX Bot webhook updates.
type Handler struct {
	client       *Client
	chatRepo     *repository.ChatRepository
	usersClient  userspb.UserServiceClient
	healthClient healthpb.HealthServiceClient
	nc           *nats.Conn
	siteURL      string
}

func NewHandler(
	client *Client,
	chatRepo *repository.ChatRepository,
	usersClient userspb.UserServiceClient,
	healthClient healthpb.HealthServiceClient,
	nc *nats.Conn,
	siteURL string,
) *Handler {
	return &Handler{
		client:       client,
		chatRepo:     chatRepo,
		usersClient:  usersClient,
		healthClient: healthClient,
		nc:           nc,
		siteURL:      siteURL,
	}
}

// --- Resty request type for the webhook endpoint ---

type WebhookRequest struct {
	Update Update
}

func (WebhookRequest) Validate() (bool, string, string) { return true, "", "" }
func (WebhookRequest) Methods() []string                { return []string{"POST"} }
func (WebhookRequest) Path() (string, bool)             { return "/max/webhook", false }
func (WebhookRequest) String() string                   { return "max-webhook" }

func previewStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

func NewWebhookRequest(ctx context.Context, r *http.Request) (context.Context, WebhookRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ctx, WebhookRequest{}, err
	}
	var update Update
	ctx = obs.WithTrace(ctx)
	if err := json.Unmarshal(body, &update); err != nil {
		logger.Error(ctx, err, "max webhook unmarshal", "body_len", len(body), "body_preview", previewStr(string(body), 2000))
	} else {
		logger.Info(ctx, "max webhook request",
			"body_len", len(body), "update_type", update.UpdateType,
			"text_preview", previewStr(func() string {
				if update.Message != nil {
					return update.Message.Body.Text
				}
				return ""
			}(), 200),
			"payload", previewStr(func() string {
				if update.Callback != nil {
					return update.Callback.Payload
				}
				return ""
			}(), 500),
		)
	}
	return ctx, WebhookRequest{Update: update}, nil
}

// HandleWebhook is the resty endpoint handler for POST /max/webhook.
func (h *Handler) HandleWebhook(ctx context.Context, req WebhookRequest) (responses.Response, int) {
	logger.Info(ctx, "max HandleWebhook", "update_type", req.Update.UpdateType)
	switch req.Update.UpdateType {
	case "message_created":
		if req.Update.Message != nil {
			h.handleMessage(ctx, req.Update.Message)
		} else {
			logger.Warn(ctx, "max HandleWebhook: message_created but Message is nil")
		}
	case "message_callback":
		if req.Update.Callback != nil {
			h.handleCallback(ctx, req.Update.Callback)
		} else {
			logger.Warn(ctx, "max HandleWebhook: message_callback but Callback is nil")
		}
	case "bot_started":
		h.handleBotStarted(ctx, &req.Update)
	default:
		logger.Warn(ctx, "max HandleWebhook: unhandled update_type", "update_type", req.Update.UpdateType)
	}
	return &responses.SuccessResponse{Success: true}, 200
}

func (h *Handler) handleBotStarted(ctx context.Context, update *Update) {
	if update.User == nil {
		return
	}
	chatID := update.ChatID
	if chatID == 0 {
		return
	}

	payload := strings.TrimSpace(update.Payload)
	if payload != "" {
		msg := &Message{
			Sender:    *update.User,
			Recipient: Recipient{ChatID: chatID},
		}
		h.handleStartWithKey(ctx, msg, payload)
	} else {
		msg := &Message{
			Sender:    *update.User,
			Recipient: Recipient{ChatID: chatID},
		}
		h.handleStartWithoutKey(ctx, msg)
	}
}

func (h *Handler) handleMessage(ctx context.Context, msg *Message) {
	phone := extractPhone(msg)
	if phone != "" {
		h.handleContact(ctx, msg, phone)
		return
	}

	maxUserID := strconv.FormatInt(msg.Sender.UserID, 10)
	text := strings.TrimSpace(msg.Body.Text)

	// "cancel" resets all criteria.
	if strings.EqualFold(text, "отмена") || strings.EqualFold(text, "cancel") {
		pendingInput.Delete(maxUserID)
		h.clearMaxLabUpload(maxUserID)
		h.clearAnalysisPickMax(maxUserID)
		h.handleCancelAll(ctx, msg)
		return
	}

	// Check for pending input.
	if val, ok := pendingInput.LoadAndDelete(maxUserID); ok {
		pending := val.(PendingInput)
		h.handleNumericInput(ctx, msg, pending)
		return
	}

	if h.tryHandleMaxLabMessage(ctx, msg) {
		return
	}

	if strings.HasPrefix(text, "/start") {
		parts := strings.SplitN(text, " ", 2)
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			h.handleStartWithKey(ctx, msg, strings.TrimSpace(parts[1]))
		} else {
			h.handleStartWithoutKey(ctx, msg)
		}
		return
	}

	if text == "/password" {
		h.handlePasswordCommand(ctx, msg)
		return
	}

	if h.handleMaxAnalysisPickReply(ctx, msg, maxUserID) {
		return
	}

	h.sendMainMenu(ctx, msg.Recipient.ChatID)
}

func (h *Handler) handleCallback(ctx context.Context, cb *Callback) {
	if err := h.client.AnswerCallback(cb.CallbackID); err != nil {
		logger.Error(ctx, err, "max answer callback", "callback_id", cb.CallbackID)
	}

	var chatID int64
	if cb.Message != nil {
		chatID = cb.Message.Recipient.ChatID
	}
	if chatID == 0 {
		maxUserID := strconv.FormatInt(cb.User.UserID, 10)
		chat, err := h.chatRepo.FindByMaxUserID(ctx, maxUserID)
		if err == nil && chat.ChatID != nil {
			chatID, _ = strconv.ParseInt(*chat.ChatID, 10, 64)
		}
	}
	if chatID == 0 {
		logger.Error(ctx, fmt.Errorf("no chatID"), "max handleCallback: no chatID", "user_id", cb.User.UserID, "payload", cb.Payload)
		return
	}

	payload := cb.Payload
	switch {
	case strings.HasPrefix(payload, "menu:"):
		h.handleMenuCallback(ctx, cb, chatID)
	case strings.HasPrefix(payload, "data:"):
		h.handleDataCallback(ctx, cb, chatID)
	case strings.HasPrefix(payload, "input:"):
		h.handleInputCallback(ctx, cb, chatID)
	case strings.HasPrefix(payload, "onboard:"):
		h.handleOnboardingCallback(ctx, cb, chatID)
	case strings.HasPrefix(payload, "lab:"):
		h.handleLabMaxCallbacks(ctx, cb, chatID)
	case payload == "rec:weekly":
		maxUserID := strconv.FormatInt(cb.User.UserID, 10)
		h.handleWeeklyRecommendations(ctx, chatID, maxUserID)
	}
}
