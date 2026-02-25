// Package gateway — Slack adapter for ZBOT.
// Uses Socket Mode so no public URL or port forwarding needed.
// ZBOT connects outbound to Slack — works from anywhere, including behind NAT.
package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// Attachment holds file data downloaded from Slack.
type Attachment struct {
	Data      []byte
	MediaType string // "image/jpeg", "image/png", "application/pdf", etc.
	Filename  string
}

// SlackGateway implements agent.Gateway using Slack Socket Mode.
// No webhook URL needed — pure outbound WebSocket connection.
type SlackGateway struct {
	client       *slack.Client
	socket       *socketmode.Client
	botToken     string
	allowedUsers map[string]bool // only these Slack user IDs can trigger ZBOT
	handler      MessageHandler
	logger       *slog.Logger
}

// MessageHandler is called when a valid DM arrives.
// The gateway hands off to this — decoupled from the agent loop.
type MessageHandler func(ctx context.Context, sessionID, userID, text string, attachments []Attachment) (string, error)

// NewSlackGateway constructs the gateway.
// botToken: xoxb-... (Bot User OAuth Token)
// appToken: xapp-... (App-Level Token with connections:write scope — for Socket Mode)
// allowedUsers: Slack user IDs that can message ZBOT (your user ID)
func NewSlackGateway(
	botToken, appToken string,
	allowedUsers []string,
	handler MessageHandler,
	logger *slog.Logger,
) *SlackGateway {
	api := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
	)
	socket := socketmode.New(api)

	allowed := make(map[string]bool, len(allowedUsers))
	for _, u := range allowedUsers {
		allowed[u] = true
	}

	return &SlackGateway{
		client:       api,
		socket:       socket,
		botToken:     botToken,
		allowedUsers: allowed,
		handler:      handler,
		logger:       logger,
	}
}

// Start connects to Slack via Socket Mode and begins processing events.
// Blocks until ctx is cancelled.
func (g *SlackGateway) Start(ctx context.Context) error {
	g.logger.Info("Slack gateway connecting via Socket Mode")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-g.socket.Events:
				if !ok {
					return
				}
				g.handleEvent(ctx, evt)
			}
		}
	}()

	return g.socket.RunContext(ctx)
}

// Send delivers a text response to a Slack channel/DM.
func (g *SlackGateway) Send(ctx context.Context, sessionID, content string) error {
	_, _, err := g.client.PostMessageContext(ctx, sessionID,
		slack.MsgOptionText(content, false),
	)
	if err != nil {
		return fmt.Errorf("slack.Send channel=%s: %w", sessionID, err)
	}
	return nil
}

// SendFile delivers a file to a Slack channel/DM.
func (g *SlackGateway) SendFile(ctx context.Context, sessionID, filename string, data []byte) error {
	params := slack.UploadFileParameters{
		Channel:  sessionID,
		Filename: filename,
		Content:  string(data),
	}
	_, err := g.client.UploadFile(params)
	if err != nil {
		return fmt.Errorf("slack.SendFile channel=%s: %w", sessionID, err)
	}
	return nil
}

// handleEvent processes incoming Slack events.
func (g *SlackGateway) handleEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		g.logger.Info("Slack Socket Mode connecting...")

	case socketmode.EventTypeConnected:
		g.logger.Info("Slack Socket Mode connected ✓")

	case socketmode.EventTypeEventsAPI:
		g.socket.Ack(*evt.Request)
		eventsPayload, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		g.handleEventsAPI(ctx, eventsPayload)

	default:
		// ignore other event types
	}
}

// supportedMediaTypes lists MIME types ZBOT accepts from Slack file uploads.
var supportedMediaTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/gif":       true,
	"image/webp":      true,
	"application/pdf": true,
}

// maxFileSize is the maximum file size ZBOT will download (20MB).
const maxFileSize = 20 * 1024 * 1024

// handleEventsAPI processes Events API payloads (messages, DMs).
func (g *SlackGateway) handleEventsAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch event.InnerEvent.Type {
	case "message", "app_mention":
		msgEvent, ok := event.InnerEvent.Data.(*slackevents.MessageEvent)
		if !ok {
			return
		}

		// Ignore bot messages (including our own) to prevent loops.
		if msgEvent.BotID != "" || msgEvent.SubType == "bot_message" {
			return
		}

		// Enforce allowlist — only permitted users can trigger ZBOT.
		if len(g.allowedUsers) > 0 && !g.allowedUsers[msgEvent.User] {
			g.logger.Warn("message from non-allowed user", "user", msgEvent.User)
			return
		}

		// Download any file attachments (files appear in msgEvent.Message for new messages).
		var attachments []Attachment
		if msgEvent.Message != nil && len(msgEvent.Message.Files) > 0 {
			attachments = g.downloadFiles(ctx, msgEvent.Message.Files)
		}

		// Allow messages that have either text or attachments (or both).
		if msgEvent.Text == "" && len(attachments) == 0 {
			return
		}

		sessionID := msgEvent.Channel
		userID := msgEvent.User
		text := msgEvent.Text

		g.logger.Info("message received",
			"user", userID,
			"channel", sessionID,
			"text_len", len(text),
			"files", len(attachments),
		)

		// Send typing indicator.
		g.client.PostMessage(sessionID, slack.MsgOptionText("_thinking..._", false))

		// Hand off to the agent handler.
		go func() {
			reply, err := g.handler(ctx, sessionID, userID, text, attachments)
			if err != nil {
				g.logger.Error("handler error", "err", err)
				g.Send(ctx, sessionID, fmt.Sprintf("❌ Error: %v", err))
				return
			}
			if err := g.Send(ctx, sessionID, reply); err != nil {
				g.logger.Error("send error", "err", err)
			}
		}()
	}
}

// downloadFiles downloads file attachments from Slack using the bot token.
func (g *SlackGateway) downloadFiles(ctx context.Context, files []slack.File) []Attachment {
	var attachments []Attachment

	for _, f := range files {
		// Check if the media type is supported.
		if !supportedMediaTypes[f.Mimetype] {
			g.logger.Info("skipping unsupported file type", "type", f.Mimetype, "name", f.Name)
			continue
		}

		// Check file size.
		if f.Size > maxFileSize {
			g.logger.Warn("file too large, skipping", "name", f.Name, "size", f.Size)
			continue
		}

		// Download using the bot token for authorization.
		data, err := g.downloadFile(ctx, f.URLPrivateDownload)
		if err != nil {
			g.logger.Error("file download failed", "name", f.Name, "err", err)
			continue
		}

		attachments = append(attachments, Attachment{
			Data:      data,
			MediaType: f.Mimetype,
			Filename:  f.Name,
		})

		g.logger.Info("file downloaded", "name", f.Name, "type", f.Mimetype, "size", len(data))
	}

	return attachments
}

// downloadFile fetches a file from Slack's private URL using the bot token.
func (g *SlackGateway) downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d downloading file", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("file exceeds 20MB limit")
	}

	return data, nil
}

// Ensure SlackGateway implements agent.Gateway.
var _ agent.Gateway = (*SlackGateway)(nil)
