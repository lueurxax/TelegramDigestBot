package linkresolver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/linkextract"
)

type TelegramResolver struct {
	client      *telegram.Client
	database    *db.DB
	rateLimiter *rate.Limiter
}

type TelegramContent struct {
	ChannelTitle    string
	ChannelUsername string
	MessageID       int64
	Text            string
	Date            time.Time
	Views           int
	Forwards        int
	HasMedia        bool
	MediaType       string
}

// ErrClientNotInitialized indicates the telegram client is not initialized.
var ErrClientNotInitialized = errors.New("telegram client not initialized")

// ErrUnsupportedTelegramLinkType indicates an unsupported telegram link type.
var ErrUnsupportedTelegramLinkType = errors.New("unsupported telegram link type")

// ErrNoUsernameOrChannelID indicates no username or channel ID was provided.
var ErrNoUsernameOrChannelID = errors.New("no username or channel ID")

// ErrMessageNotFound indicates the message was not found.
var ErrMessageNotFound = errors.New("message not found")

// ErrUnexpectedMessageType indicates an unexpected message type.
var ErrUnexpectedMessageType = errors.New("unexpected message type")

// ErrChannelNotFound indicates the channel was not found.
var ErrChannelNotFound = errors.New("channel not found")

// ErrNotAChannel indicates the peer is not a channel.
var ErrNotAChannel = errors.New("not a channel")

// ErrPrivateChannelNotTracked indicates a private channel is not tracked.
var ErrPrivateChannelNotTracked = errors.New("private channel not tracked")

func NewTelegramResolver(client *telegram.Client, database *db.DB) *TelegramResolver {
	return &TelegramResolver{
		client:      client,
		database:    database,
		rateLimiter: rate.NewLimiter(rate.Limit(0.5), 3), // 30 req/min for Telegram
	}
}

func (r *TelegramResolver) Resolve(ctx context.Context, link *linkextract.Link) (*TelegramContent, error) {
	if r.client == nil {
		return nil, ErrClientNotInitialized
	}

	if link.TelegramType != "post" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedTelegramLinkType, link.TelegramType)
	}

	if err := r.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	api := tg.NewClient(r.client)

	// Resolve channel
	var inputChannel *tg.InputChannel

	var err error

	if link.Username != "" {
		inputChannel, err = r.resolveByUsername(ctx, api, link.Username)
	} else if link.ChannelID != 0 {
		inputChannel, err = r.resolveByID(ctx, link.ChannelID)
	} else {
		return nil, ErrNoUsernameOrChannelID
	}

	if err != nil {
		return nil, err
	}

	// Fetch message
	messages, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
		Channel: inputChannel,
		ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: int(link.MessageID)}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	channelMessages, ok := messages.(*tg.MessagesChannelMessages)
	if !ok || len(channelMessages.Messages) == 0 {
		return nil, ErrMessageNotFound
	}

	msg, ok := channelMessages.Messages[0].(*tg.Message)
	if !ok {
		return nil, ErrUnexpectedMessageType
	}

	// Get channel info
	var channelTitle, channelUsername string

	for _, chat := range channelMessages.Chats {
		if ch, ok := chat.(*tg.Channel); ok {
			channelTitle = ch.Title
			channelUsername = ch.Username
			break
		}
	}

	result := &TelegramContent{
		ChannelTitle:    channelTitle,
		ChannelUsername: channelUsername,
		MessageID:       link.MessageID,
		Text:            msg.Message,
		Date:            time.Unix(int64(msg.Date), 0),
		Views:           msg.Views,
		Forwards:        msg.Forwards,
	}

	if msg.Media != nil {
		result.HasMedia = true

		switch msg.Media.(type) {
		case *tg.MessageMediaPhoto:
			result.MediaType = "photo"
		case *tg.MessageMediaDocument:
			result.MediaType = "document"
		default:
			result.MediaType = "other"
		}
	}

	return result, nil
}

func (r *TelegramResolver) resolveByUsername(ctx context.Context, api *tg.Client, username string) (*tg.InputChannel, error) {
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: username,
	})
	if err != nil {
		return nil, err
	}

	if len(resolved.Chats) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrChannelNotFound, username)
	}

	channel, ok := resolved.Chats[0].(*tg.Channel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotAChannel, username)
	}

	return &tg.InputChannel{
		ChannelID:  channel.ID,
		AccessHash: channel.AccessHash,
	}, nil
}

func (r *TelegramResolver) resolveByID(ctx context.Context, channelID int64) (*tg.InputChannel, error) {
	// Check if we're tracking this channel
	ch, err := r.database.GetChannelByPeerID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("%w: %d", ErrPrivateChannelNotTracked, channelID)
	}

	return &tg.InputChannel{
		ChannelID:  ch.TGPeerID,
		AccessHash: ch.AccessHash,
	}, nil
}
