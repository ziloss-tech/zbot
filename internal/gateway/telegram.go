// Package gateway implements the Telegram adapter (agent.Gateway).
// The web UI adapter (loopback-only, Tailscale-gated) is in webui.go.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jeremylerwick-max/zbot/internal/agent"
	"golang.org/x/time/rate"
)

// TelegramGateway implements agent.Gateway for Telegram.
// Security controls:
//   - AllowFrom whitelist: only listed user IDs can send messages
//   - Rate limiter: 10 req/min per user to protect Claude API quota
//   - No inline mode, no group handling in v1 (private chats only)
type TelegramGateway struct {
	bot        *tgbotapi.BotAPI
	allowFrom  map[int64]struct{} // whitelisted Telegram user IDs
	handler    MessageHandler
	limiters   map[int64]*rate.Limiter
	logger     *slog.Logger
}

// MessageHandler is called for every verified inbound message.
// The gateway calls this; the agent loop lives here.
type MessageHandler func(ctx context.Context, sessionID string, msg agent.Message) (*agent.TurnOutput, error)

// TurnOutput re-exported for gateway use — avoid import cycle by using agent.TurnOutput directly.

// NewTelegramGateway constructs the adapter.
// token: Telegram bot token (from GCP Secret Manager, NOT passed as literal)
// allowFrom: slice of allowed Telegram user IDs
func NewTelegramGateway(token string, allowFrom []int64, handler MessageHandler, logger *slog.Logger) (*TelegramGateway, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("gateway.NewTelegramGateway: %w", err)
	}

	allowMap := make(map[int64]struct{}, len(allowFrom))
	for _, id := range allowFrom {
		allowMap[id] = struct{}{}
	}

	logger.Info("telegram bot initialized", "username", bot.Self.UserName)

	return &TelegramGateway{
		bot:       bot,
		allowFrom: allowMap,
		handler:   handler,
		limiters:  make(map[int64]*rate.Limiter),
		logger:    logger,
	}, nil
}

// Start begins the update polling loop. Blocks until ctx is cancelled.
func (g *TelegramGateway) Start(ctx context.Context) error {
	cfg := tgbotapi.NewUpdate(0)
	cfg.Timeout = 60

	updates := g.bot.GetUpdatesChan(cfg)

	g.logger.Info("telegram gateway listening")

	for {
		select {
		case <-ctx.Done():
			g.bot.StopReceivingUpdates()
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			go g.handleUpdate(ctx, update)
		}
	}
}

// handleUpdate processes a single Telegram update.
func (g *TelegramGateway) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	msg := update.Message
	userID := msg.From.ID

	// Security: reject messages from non-whitelisted users.
	if _, ok := g.allowFrom[userID]; !ok {
		g.logger.Warn("rejected message from non-whitelisted user", "user_id", userID)
		return
	}

	// Rate limit per user.
	limiter := g.getLimiter(userID)
	if !limiter.Allow() {
		g.logger.Warn("rate limit exceeded", "user_id", userID)
		g.sendText(msg.Chat.ID, "⏳ Slow down a bit — you're hitting the rate limit.")
		return
	}

	sessionID := strconv.FormatInt(msg.Chat.ID, 10)

	// Build agent.Message — handle text and images.
	agentMsg := agent.Message{
		Role:    agent.RoleUser,
		Content: msg.Text,
	}

	// Photo handling — attach largest available photo.
	if msg.Photo != nil && len(msg.Photo) > 0 {
		largest := msg.Photo[len(msg.Photo)-1]
		fileURL, err := g.bot.GetFileDirectURL(largest.FileID)
		if err == nil {
			// TODO Sprint 2: download image bytes and attach to agentMsg.Images
			agentMsg.Content += fmt.Sprintf("\n[image attached: %s]", fileURL)
		}
	}

	// Send typing indicator — long operations take time.
	_, _ = g.bot.Send(tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping))

	result, err := g.handler(ctx, sessionID, agentMsg)
	if err != nil {
		g.logger.Error("handler error", "session", sessionID, "err", err)
		g.sendText(msg.Chat.ID, "❌ Something went wrong. Check logs.")
		return
	}

	if result == nil {
		return
	}

	// Send text reply — split if over Telegram's 4096 char limit.
	if result.Reply != "" {
		g.sendSplitText(msg.Chat.ID, result.Reply)
	}

	// Send any output files as documents.
	for _, f := range result.Files {
		g.sendDocument(msg.Chat.ID, f.Name, f.Data)
	}
}

// Send implements agent.Gateway.
func (g *TelegramGateway) Send(ctx context.Context, sessionID, content string) error {
	chatID, err := strconv.ParseInt(sessionID, 10, 64)
	if err != nil {
		return fmt.Errorf("gateway.Send invalid sessionID %q: %w", sessionID, err)
	}
	g.sendText(chatID, content)
	return nil
}

// SendFile implements agent.Gateway.
func (g *TelegramGateway) SendFile(ctx context.Context, sessionID, filename string, data []byte) error {
	chatID, err := strconv.ParseInt(sessionID, 10, 64)
	if err != nil {
		return fmt.Errorf("gateway.SendFile invalid sessionID %q: %w", sessionID, err)
	}
	g.sendDocument(chatID, filename, data)
	return nil
}

// sendText sends a plain text message with Markdown parse mode.
func (g *TelegramGateway) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := g.bot.Send(msg); err != nil {
		g.logger.Error("sendText failed", "chat_id", chatID, "err", err)
	}
}

// sendSplitText splits messages longer than 4096 chars.
func (g *TelegramGateway) sendSplitText(chatID int64, text string) {
	const maxLen = 4000
	for len(text) > maxLen {
		// Find a good split point (newline before limit).
		split := maxLen
		for i := maxLen; i > maxLen-200 && i > 0; i-- {
			if text[i] == '\n' {
				split = i
				break
			}
		}
		g.sendText(chatID, text[:split])
		text = text[split:]
	}
	if len(text) > 0 {
		g.sendText(chatID, text)
	}
}

// sendDocument sends a file as a Telegram document.
func (g *TelegramGateway) sendDocument(chatID int64, filename string, data []byte) {
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  filename,
		Bytes: data,
	})
	if _, err := g.bot.Send(doc); err != nil {
		g.logger.Error("sendDocument failed", "chat_id", chatID, "filename", filename, "err", err)
	}
}

// getLimiter returns or creates a rate limiter for a user.
// 10 messages per minute — protects Claude API quota.
func (g *TelegramGateway) getLimiter(userID int64) *rate.Limiter {
	if l, ok := g.limiters[userID]; ok {
		return l
	}
	// 10 tokens, refills at 1/6 per second (10/min)
	l := rate.NewLimiter(rate.Limit(1.0/6.0), 10)
	g.limiters[userID] = l
	return l
}

// Ensure TelegramGateway implements the port.
var _ agent.Gateway = (*TelegramGateway)(nil)
