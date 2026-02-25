// Package gateway — Slack adapter for ZBOT.
// Uses Socket Mode so no public URL or port forwarding needed.
// ZBOT connects outbound to Slack — works from anywhere, including behind NAT.
package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// SlackGateway implements agent.Gateway using Slack Socket Mode.
// No webhook URL needed — pure outbound WebSocket connection.
type SlackGateway struct {
	client       *slack.Client
	socket       *socketmode.Client
	allowedUsers map[string]bool // only these Slack user IDs can trigger ZBOT
	handler      MessageHandler
	logger       *slog.Logger
}

// MessageHandler is called when a valid DM arrives.
// The gateway hands off to this — decoupled from the agent loop.
type MessageHandler func(ctx context.Context, sessionID, userID, text string) (string, error)

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

		if msgEvent.Text == "" {
			return
		}

		sessionID := msgEvent.Channel
		userID := msgEvent.User
		text := msgEvent.Text

		g.logger.Info("message received",
			"user", userID,
			"channel", sessionID,
			"text_len", len(text),
		)

		// Send typing indicator.
		g.client.PostMessage(sessionID, slack.MsgOptionText("_thinking..._", false))

		// Hand off to the agent handler.
		go func() {
			reply, err := g.handler(ctx, sessionID, userID, text)
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

// Ensure SlackGateway implements agent.Gateway.
var _ agent.Gateway = (*SlackGateway)(nil)

// bytesReader wraps []byte as an io.Reader.
type bytesReader []byte

func (b *bytesReader) Read(p []byte) (n int, err error) {
	if len(*b) == 0 {
		return 0, fmt.Errorf("EOF")
	}
	n = copy(p, *b)
	*b = (*b)[n:]
	return n, nil
}
