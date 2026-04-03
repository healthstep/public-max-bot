package bot

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-max-bot/internal/repository"
	"github.com/nats-io/nats.go"
	"github.com/porebric/resty/responses"
)

// pendingInput tracks users who are in the middle of typing a numeric value.
var pendingInput sync.Map

type PendingInput struct {
	CriterionID   string
	CriterionName string
}

// Handler processes MAX Bot webhook updates.
type Handler struct {
	client       *Client
	chatRepo     *repository.ChatRepository
	usersClient  userspb.UserServiceClient
	healthClient healthpb.HealthServiceClient
	nc           *nats.Conn
}

func NewHandler(
	client *Client,
	chatRepo *repository.ChatRepository,
	usersClient userspb.UserServiceClient,
	healthClient healthpb.HealthServiceClient,
	nc *nats.Conn,
) *Handler {
	return &Handler{
		client:       client,
		chatRepo:     chatRepo,
		usersClient:  usersClient,
		healthClient: healthClient,
		nc:           nc,
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

func NewWebhookRequest(ctx context.Context, r *http.Request) (context.Context, WebhookRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ctx, WebhookRequest{}, err
	}
	var update Update
	_ = json.Unmarshal(body, &update)
	return ctx, WebhookRequest{Update: update}, nil
}

// HandleWebhook is the resty endpoint handler for POST /max/webhook.
func (h *Handler) HandleWebhook(ctx context.Context, req WebhookRequest) (responses.Response, int) {
	switch req.Update.UpdateType {
	case "message_created":
		if req.Update.Message != nil {
			h.handleMessage(ctx, req.Update.Message)
		}
	case "message_callback":
		if req.Update.Callback != nil {
			h.handleCallback(ctx, req.Update.Callback)
		}
	case "bot_started":
		h.handleBotStarted(ctx, &req.Update)
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

	// Check for pending numeric input.
	if val, ok := pendingInput.LoadAndDelete(maxUserID); ok {
		pending := val.(PendingInput)
		h.handleNumericInput(ctx, msg, pending)
		return
	}

	text := strings.TrimSpace(msg.Body.Text)

	if strings.HasPrefix(text, "/start") {
		parts := strings.SplitN(text, " ", 2)
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			h.handleStartWithKey(ctx, msg, strings.TrimSpace(parts[1]))
		} else {
			h.handleStartWithoutKey(ctx, msg)
		}
		return
	}

	h.sendMainMenu(ctx, msg.Recipient.ChatID)
}

func (h *Handler) handleCallback(ctx context.Context, cb *Callback) {
	if err := h.client.AnswerCallback(cb.CallbackID); err != nil {
		log.Printf("answer callback %s: %v", cb.CallbackID, err)
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
	}
}
