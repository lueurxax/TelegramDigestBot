package telegramreader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/linkextract"
	"github.com/lueurxax/telegram-digest-bot/internal/linkresolver"
	"github.com/lueurxax/telegram-digest-bot/internal/observability"
	"github.com/rs/zerolog"
)

// ErrChannelNotFound indicates the channel was not found.
var ErrChannelNotFound = errors.New("channel not found")

// ErrNotAChannel indicates the peer is not a channel.
var ErrNotAChannel = errors.New("peer is not a channel")

// ErrMissingAccessHash indicates the channel is missing an access hash.
var ErrMissingAccessHash = errors.New("missing access_hash for channel")

// ErrNoChannelIdentifier indicates no username, ID, or invite link is available.
var ErrNoChannelIdentifier = errors.New("channel has no username, ID or invite link")

// ErrUnexpectedInviteType indicates an unexpected invite type was returned.
var ErrUnexpectedInviteType = errors.New("chat invite returned unexpected type")

type Reader struct {
	cfg         *config.Config
	database    *db.DB
	client      *telegram.Client
	resolver    *linkresolver.Resolver
	logger      *zerolog.Logger
	downloadSem chan struct{}
	// Worker pool for parallel channel processing
	workerSem chan struct{}
}

func New(cfg *config.Config, database *db.DB, logger *zerolog.Logger) *Reader {
	// Calculate optimal worker count based on rate limit
	// With RateLimitRPS=1, we can process 1 channel per second
	// With RateLimitRPS=5, we can process 5 channels per second (with staggered delays)
	workerCount := cfg.RateLimitRPS

	if workerCount < 1 {
		workerCount = 1
	}

	if workerCount > 10 {
		workerCount = 10 // Cap at 10 to avoid overwhelming Telegram API
	}

	return &Reader{
		cfg:         cfg,
		database:    database,
		logger:      logger,
		downloadSem: make(chan struct{}, 5),           // limit to 5 concurrent downloads
		workerSem:   make(chan struct{}, workerCount), // limit concurrent channel fetches
	}
}

func (r *Reader) Run(ctx context.Context) error {
	client := telegram.NewClient(r.cfg.TGAPIID, r.cfg.TGAPIHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: r.cfg.TGSessionPath,
		},
	})

	r.client = client

	return client.Run(ctx, func(ctx context.Context) error {
		err := client.Auth().IfNecessary(ctx, r.authFlow())
		if err != nil {
			return err
		}

		r.logger.Info().Msg("Successfully authenticated as user")

		r.resolver = linkresolver.New(r.cfg, r.database, client, r.logger)

		// Start tracking channels and ingesting messages
		return r.ingestMessages(ctx)
	})
}

func (r *Reader) ingestMessages(ctx context.Context) error {
	api := tg.NewClient(r.client)

	for { //nolint:wsl
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		channels, err := r.database.GetActiveChannels(ctx)
		if err != nil {
			r.logger.Error().Err(err).Msg("failed to get active channels")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}

			continue
		}

		if len(channels) == 0 {
			r.logger.Info().Msg("No active channels to track. Waiting...")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
			}

			continue
		}

		r.logger.Info().Int("channels", len(channels)).Msg("Starting ingestion cycle")
		start := time.Now()

		// Calculate minimum delay between API calls based on RateLimitRPS
		minDelay := 1000 * time.Millisecond
		if r.cfg.RateLimitRPS > 0 {
			minDelay = time.Duration(1000/r.cfg.RateLimitRPS) * time.Millisecond
		}

		// Use channels for collecting results
		type fetchResult struct {
			channel string
			count   int
			err     error
		}

		results := make(chan fetchResult, len(channels))

		// Process channels with worker pool
		for _, ch := range channels {
			ch := ch // capture for goroutine

			// Acquire worker slot (blocks if all workers busy)
			select {
			case r.workerSem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			go func() {
				defer func() { <-r.workerSem }() // Release worker slot

				// Rate limiting delay with jitter
				jitter := minDelay + time.Duration(float64(minDelay)*0.5*(float64(time.Now().UnixNano()%1000)/1000.0))

				select {
				case <-ctx.Done():
					results <- fetchResult{channel: ch.Username, err: ctx.Err()}

					return
				case <-time.After(jitter):
				}

				msgs, err := r.fetchChannelMessages(ctx, api, ch)
				results <- fetchResult{channel: ch.Username, count: msgs, err: err}
			}()
		}

		// Collect results
		cycleMsgs := 0

		for i := 0; i < len(channels); i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case result := <-results:
				if result.err != nil {
					r.logger.Error().Str("channel", result.channel).Err(result.err).Msg("failed to fetch messages for channel")
				}

				cycleMsgs += result.count
			}
		}

		r.logger.Info().Int("channels", len(channels)).Int("msgs", cycleMsgs).Dur("duration", time.Since(start)).Msg("Finished ingestion cycle")

		// Resolve unknown discoveries (channels with peer ID but no title)
		go r.resolveUnknownDiscoveries(ctx, api)

		// Resolve invite link discoveries
		go r.resolveInviteLinkDiscoveries(ctx, api)

		// Adaptive delay: shorter if we found messages, longer if quiet
		cycleDelay := 30 * time.Second
		if cycleMsgs > 0 {
			cycleDelay = 15 * time.Second // Poll more frequently if active
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cycleDelay):
		}
	}
}

// resolveUnknownDiscoveries attempts to fetch channel info for discoveries with peer IDs but no titles
func (r *Reader) resolveUnknownDiscoveries(ctx context.Context, api *tg.Client) {
	discoveries, err := r.database.GetDiscoveriesNeedingResolution(ctx, 10)
	if err != nil {
		r.logger.Warn().Err(err).Msg("failed to get discoveries needing resolution")
		return
	}

	if len(discoveries) == 0 {
		return
	}

	r.logger.Debug().Int("count", len(discoveries)).Msg("Resolving unknown discoveries")

	for _, d := range discoveries {
		// Try to get channel info using InputChannel with peer ID and access hash
		channels, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
			&tg.InputChannel{
				ChannelID:  d.TGPeerID,
				AccessHash: d.AccessHash, // Use cached access hash if available
			},
		})
		if err != nil {
			r.logger.Debug().Err(err).Int64("peer_id", d.TGPeerID).Int64("access_hash", d.AccessHash).Msg("failed to resolve channel (may be private)")

			// Increment attempt counter so we don't keep trying forever
			if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
				r.logger.Warn().Err(err).Msg("failed to increment resolution attempts")
			}

			time.Sleep(200 * time.Millisecond)

			continue
		}

		// Extract channel info from response
		resolved := false

		if channelsResult, ok := channels.(*tg.MessagesChats); ok {
			for _, chat := range channelsResult.Chats {
				if channel, ok := chat.(*tg.Channel); ok && channel.ID == d.TGPeerID {
					r.logger.Info().
						Int64("peer_id", d.TGPeerID).
						Str("title", channel.Title).
						Str("username", channel.Username).
						Msg("Resolved unknown discovery")

					if err := r.database.UpdateDiscoveryChannelInfo(ctx, d.ID, channel.Title, channel.Username); err != nil {
						r.logger.Warn().Err(err).Msg("failed to update discovery info")
					}

					resolved = true

					break
				}
			}
		}

		if !resolved {
			// API call succeeded but didn't return our channel - mark as attempted
			if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
				r.logger.Warn().Err(err).Msg("failed to increment resolution attempts")
			}
		}

		// Rate limit to avoid hitting Telegram API limits
		time.Sleep(500 * time.Millisecond)
	}
}

// resolveInviteLinkDiscoveries attempts to fetch channel info for discoveries with invite links
func (r *Reader) resolveInviteLinkDiscoveries(ctx context.Context, api *tg.Client) {
	discoveries, err := r.database.GetInviteLinkDiscoveriesNeedingResolution(ctx, 5)
	if err != nil {
		r.logger.Warn().Err(err).Msg("failed to get invite link discoveries needing resolution")
		return
	}

	if len(discoveries) == 0 {
		return
	}

	r.logger.Debug().Int("count", len(discoveries)).Msg("Resolving invite link discoveries")

	for _, d := range discoveries {
		// Extract hash from invite link (e.g., https://t.me/+abc123 -> abc123)
		hash := strings.TrimPrefix(d.InviteLink, "https://t.me/+")
		hash = strings.TrimPrefix(hash, "https://t.me/joinchat/")

		if hash == d.InviteLink || hash == "" {
			r.logger.Debug().Str("invite_link", d.InviteLink).Msg("invalid invite link format")

			if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
				r.logger.Warn().Err(err).Msg("failed to increment resolution attempts")
			}

			continue
		}

		// Try to check the invite without joining
		invite, err := api.MessagesCheckChatInvite(ctx, hash)
		if err != nil {
			r.logger.Debug().Err(err).Str("invite_link", d.InviteLink).Msg("failed to check invite link")

			if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
				r.logger.Warn().Err(err).Msg("failed to increment resolution attempts")
			}

			time.Sleep(500 * time.Millisecond)

			continue
		}

		// Extract channel info from invite response
		var title, username string
		var peerID, accessHash int64

		switch i := invite.(type) {
		case *tg.ChatInviteAlready:
			// We're already a member
			if channel, ok := i.Chat.(*tg.Channel); ok {
				title = channel.Title
				username = channel.Username
				peerID = channel.ID
				accessHash = channel.AccessHash
			}
		case *tg.ChatInvite:
			// We can see the invite info without joining
			title = i.Title
			// ChatInvite doesn't give us peer ID or access hash, but we get the title
		case *tg.ChatInvitePeek:
			// We can peek at the chat
			if channel, ok := i.Chat.(*tg.Channel); ok {
				title = channel.Title
				username = channel.Username
				peerID = channel.ID
				accessHash = channel.AccessHash
			}
		}

		if title != "" {
			r.logger.Info().
				Str("invite_link", d.InviteLink).
				Str("title", title).
				Str("username", username).
				Int64("peer_id", peerID).
				Msg("Resolved invite link discovery")

			if err := r.database.UpdateDiscoveryFromInvite(ctx, d.ID, title, username, peerID, accessHash); err != nil {
				r.logger.Warn().Err(err).Msg("failed to update discovery from invite")
			}
		} else {
			// Couldn't extract info
			if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
				r.logger.Warn().Err(err).Msg("failed to increment resolution attempts")
			}
		}

		// Rate limit
		time.Sleep(1 * time.Second)
	}
}

func (r *Reader) fetchChannelDescription(ctx context.Context, api *tg.Client, channelID int64, accessHash int64) (string, error) {
	fullChannel, err := api.ChannelsGetFullChannel(ctx, &tg.InputChannel{
		ChannelID:  channelID,
		AccessHash: accessHash,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get full channel: %w", err)
	}

	if full, ok := fullChannel.FullChat.(*tg.ChannelFull); ok {
		return full.About, nil
	}

	return "", nil
}

// extractDiscoveriesFromChannelFull extracts channel discoveries from ChannelFull data
// This includes linked discussion groups and links in the channel description
func (r *Reader) extractDiscoveriesFromChannelFull(ctx context.Context, api *tg.Client, channelID string, tgChannelID int64, accessHash int64) {
	fullChannel, err := api.ChannelsGetFullChannel(ctx, &tg.InputChannel{
		ChannelID:  tgChannelID,
		AccessHash: accessHash,
	})
	if err != nil {
		r.logger.Warn().Err(err).Int64("channel_id", tgChannelID).Msg("failed to get full channel for discovery")
		return
	}

	full, ok := fullChannel.FullChat.(*tg.ChannelFull)
	if !ok {
		return
	}

	var discoveries []db.Discovery

	// Extract linked discussion group
	if full.LinkedChatID != 0 {
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      full.LinkedChatID,
			SourceType:    "linked_chat",
			FromChannelID: channelID,
		})
	}

	// Extract t.me links from channel description
	if full.About != "" {
		links := linkextract.ExtractLinks(full.About)
		for _, link := range links {
			if link.Type != linkextract.LinkTypeTelegram {
				continue
			}
			switch link.TelegramType {
			case "channel", "post":
				if link.Username != "" {
					discoveries = append(discoveries, db.Discovery{
						Username:      link.Username,
						SourceType:    "description_link",
						FromChannelID: channelID,
					})
				} else if link.ChannelID != 0 {
					discoveries = append(discoveries, db.Discovery{
						TGPeerID:      link.ChannelID,
						SourceType:    "description_link",
						FromChannelID: channelID,
					})
				}
			case "invite":
				discoveries = append(discoveries, db.Discovery{
					InviteLink:    link.URL,
					SourceType:    "description_link",
					FromChannelID: channelID,
				})
			}
		}

		// Extract @mentions from channel description
		mentions := linkextract.ExtractMentions(full.About)
		for _, username := range mentions {
			discoveries = append(discoveries, db.Discovery{
				Username:      username,
				SourceType:    "description_mention",
				FromChannelID: channelID,
			})
		}
	}

	// Record all discoveries
	for _, d := range discoveries {
		if err := r.database.RecordDiscovery(ctx, d); err != nil {
			r.logger.Warn().Err(err).Str("source_type", d.SourceType).Msg("failed to record channel full discovery")
		}
	}
}

func (r *Reader) fetchChannelMessages(ctx context.Context, api *tg.Client, ch db.Channel) (int, error) {
	r.logger.Debug().Str("username", ch.Username).Str("title", ch.Title).Msg("Fetching messages for channel")

	// 1. Join by invite link if needed
	if ch.InviteLink != "" && ch.TGPeerID == 0 {
		hash := ch.InviteLink
		for _, prefix := range []string{"https://t.me/joinchat/", "https://t.me/+", "t.me/joinchat/", "t.me/+"} {
			if strings.HasPrefix(hash, prefix) {
				hash = strings.TrimPrefix(hash, prefix)
				break
			}
		}

		r.logger.Info().Str("invite_link", ch.InviteLink).Msg("Attempting to join channel by invite link")
		updates, err := api.MessagesImportChatInvite(ctx, hash)
		if err != nil {
			if !tgerr.Is(err, "USER_ALREADY_PARTICIPANT") {
				return 0, fmt.Errorf("failed to join channel by invite link: %w", err)
			}
			// If already joined, check invite to get info
			invite, err := api.MessagesCheckChatInvite(ctx, hash)
			if err != nil {
				return 0, fmt.Errorf("failed to check chat invite: %w", err)
			}

			switch i := invite.(type) {
			case *tg.ChatInviteAlready:
				if channel, ok := i.Chat.(*tg.Channel); ok {
					ch.TGPeerID = channel.ID
					ch.AccessHash = channel.AccessHash
					ch.Title = channel.Title
					ch.Username = channel.Username
					description, _ := r.fetchChannelDescription(ctx, api, ch.TGPeerID, ch.AccessHash)
					if err := r.database.UpdateChannel(ctx, ch.ID, ch.TGPeerID, ch.Title, ch.AccessHash, ch.Username, description); err != nil {
						r.logger.Error().Err(err).Msg("failed to update channel info from invite")
					}
					// Extract discoveries from channel description and linked chat (async)
					go r.extractDiscoveriesFromChannelFull(context.Background(), api, ch.ID, ch.TGPeerID, ch.AccessHash)
				}
			default:
				return 0, fmt.Errorf("%w: %T", ErrUnexpectedInviteType, invite)
			}
		} else {
			// Joined successfully, extract channel info from updates
			switch u := updates.(type) {
			case *tg.Updates:
				for _, chat := range u.Chats {
					if channel, ok := chat.(*tg.Channel); ok {
						ch.TGPeerID = channel.ID
						ch.AccessHash = channel.AccessHash
						ch.Title = channel.Title
						ch.Username = channel.Username
						description, _ := r.fetchChannelDescription(ctx, api, ch.TGPeerID, ch.AccessHash)
						if err := r.database.UpdateChannel(ctx, ch.ID, ch.TGPeerID, ch.Title, ch.AccessHash, ch.Username, description); err != nil {
							r.logger.Error().Err(err).Msg("failed to update channel info from join updates")
						}
						// Extract discoveries from channel description and linked chat (async)
						go r.extractDiscoveriesFromChannelFull(context.Background(), api, ch.ID, ch.TGPeerID, ch.AccessHash)
					}
				}
			}
		}
	}

	// Resolve peer - OPTIMIZATION: Skip API call if we already have valid peer info
	var peer tg.InputPeerClass
	resolvedNow := false

	if ch.TGPeerID != 0 && ch.AccessHash != 0 {
		// We already have valid peer info, use it directly (skip API call)
		peer = &tg.InputPeerChannel{
			ChannelID:  ch.TGPeerID,
			AccessHash: ch.AccessHash,
		}

		r.logger.Debug().Str("username", ch.Username).Int64("peer_id", ch.TGPeerID).Msg("Using cached peer info")
	} else if ch.Username != "" {
		// Need to resolve username to get peer info
		r.logger.Debug().Str("username", ch.Username).Msg("Resolving username (no cached peer info)")
		resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: ch.Username})
		if err != nil {
			return 0, fmt.Errorf("failed to resolve username: %w", err)
		}
		if len(resolved.Chats) == 0 {
			return 0, fmt.Errorf("%w: %s", ErrChannelNotFound, ch.Username)
		}
		channel, ok := resolved.Chats[0].(*tg.Channel)
		if !ok {
			return 0, fmt.Errorf("%w: %s", ErrNotAChannel, ch.Username)
		}
		// Update channel info
		r.logger.Info().Str("username", ch.Username).Int64("peer_id", channel.ID).Str("title", channel.Title).Msg("Caching channel info")
		ch.TGPeerID = channel.ID
		ch.AccessHash = channel.AccessHash
		ch.Title = channel.Title
		resolvedNow = true
		peer = &tg.InputPeerChannel{
			ChannelID:  ch.TGPeerID,
			AccessHash: ch.AccessHash,
		}
	} else if ch.TGPeerID != 0 {
		return 0, fmt.Errorf("%w: %d", ErrMissingAccessHash, ch.TGPeerID)
	} else {
		return 0, fmt.Errorf("%w: %s", ErrNoChannelIdentifier, ch.ID)
	}

	// Update channel info in DB if it was just resolved OR if description is missing
	if resolvedNow || (ch.Description == "" && ch.TGPeerID != 0 && ch.AccessHash != 0) {
		description := ch.Description
		if description == "" {
			r.logger.Info().Int64("peer_id", ch.TGPeerID).Msg("Fetching missing channel description")
			var err error
			description, err = r.fetchChannelDescription(ctx, api, ch.TGPeerID, ch.AccessHash)
			if err != nil {
				r.logger.Warn().Err(err).Int64("peer_id", ch.TGPeerID).Msg("failed to fetch channel description")
			}
		}

		// Only update if we resolved ID/Hash OR if we actually got a description
		if resolvedNow || (description != "" && ch.Description == "") {
			if err := r.database.UpdateChannel(ctx, ch.ID, ch.TGPeerID, ch.Title, ch.AccessHash, ch.Username, description); err != nil {
				r.logger.Error().Err(err).Msg("failed to update channel info")
			}
			ch.Description = description

			// Extract discoveries from channel description and linked chat (async)
			go r.extractDiscoveriesFromChannelFull(context.Background(), api, ch.ID, ch.TGPeerID, ch.AccessHash)
		}
	}

	r.logger.Debug().Str("username", ch.Username).Int64("peer_id", ch.TGPeerID).Int64("last_id", ch.LastTGMessageID).Msg("Getting history")

	req := &tg.MessagesGetHistoryRequest{
		Peer:  peer,
		Limit: r.cfg.ReaderFetchLimit,
	}

	if ch.LastTGMessageID > 0 {
		// Fetch messages newer than last seen
		req.OffsetID = int(ch.LastTGMessageID)
		req.AddOffset = -r.cfg.ReaderFetchLimit
	}

	history, err := api.MessagesGetHistory(ctx, req)
	if err != nil {
		floodErr, ok := tgerr.As(err)
		if ok && floodErr.Type == "FLOOD_WAIT" {
			r.logger.Warn().Int("seconds", floodErr.Argument).Str("channel", ch.Username).Msg("flood wait")

			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(time.Duration(floodErr.Argument) * time.Second):
			}

			return 0, nil
		}

		return 0, fmt.Errorf("failed to get history: %w", err)
	}

	var messages []tg.MessageClass
	var chats []tg.ChatClass

	switch h := history.(type) {
	case *tg.MessagesMessages:
		messages = h.Messages
		chats = h.Chats
	case *tg.MessagesMessagesSlice:
		messages = h.Messages
		chats = h.Chats
	case *tg.MessagesChannelMessages:
		messages = h.Messages
		chats = h.Chats
	case *tg.MessagesMessagesNotModified:
		r.logger.Debug().Str("channel", ch.Username).Msg("History not modified")

		return 0, nil
	}

	// Build channel title lookup map from chats in response
	channelTitles := make(map[int64]string)
	channelAccessHashes := make(map[int64]int64)

	for _, chat := range chats {
		if channel, ok := chat.(*tg.Channel); ok {
			channelTitles[channel.ID] = channel.Title
			channelAccessHashes[channel.ID] = channel.AccessHash
		}
	}

	r.logger.Debug().Str("channel", ch.Username).Int("count", len(messages)).Int("chats_in_response", len(chats)).Msg("Processing messages")

	count := 0
	maxID := ch.LastTGMessageID
	for _, m := range messages {
		// Handle service messages for discovery (channel migrations, etc.)
		if svcMsg, ok := m.(*tg.MessageService); ok {
			go func(sm *tg.MessageService, channelID string) {
				discoveries := r.extractDiscoveriesFromService(sm, channelID)
				for _, d := range discoveries {
					if err := r.database.RecordDiscovery(context.Background(), d); err != nil {
						r.logger.Warn().Err(err).Int64("peer_id", d.TGPeerID).Msg("failed to record service message discovery")
					}
				}
			}(svcMsg, ch.ID)

			continue
		}

		msg, ok := m.(*tg.Message)
		if !ok {
			continue
		}

		if msg.ID > int(maxID) {
			maxID = int64(msg.ID)
		}

		if msg.Message == "" && msg.Media == nil {
			continue
		}

		entitiesJSON, _ := json.Marshal(msg.Entities)
		mediaJSON, _ := json.Marshal(msg.Media)

		_, isForward := msg.GetFwdFrom()

		rawMsg := &db.RawMessage{
			ChannelID:     ch.ID,
			TGMessageID:   int64(msg.ID),
			TGDate:        time.Unix(int64(msg.Date), 0),
			Text:          msg.Message,
			EntitiesJSON:  entitiesJSON,
			MediaJSON:     mediaJSON,
			CanonicalHash: r.canonicalize(msg.Message),
			IsForward:     isForward,
		}

		// TODO: Consider using object storage (S3/MinIO) for media_data instead of BYTEA in PostgreSQL
		// for better scalability in high-volume production deployments.
		if err := r.database.SaveRawMessage(ctx, rawMsg); err != nil {
			r.logger.Error().Err(err).Str("channel", ch.Username).Int("msg_id", msg.ID).Msg("failed to save raw message")
		} else {
			count++

			observability.MessagesIngested.WithLabelValues(ch.Username).Inc()

			// Start async download if media exists
			if msg.Media != nil {
				go func(m tg.MessageMediaClass, rm db.RawMessage) {
					// Wait for slot
					select {
					case r.downloadSem <- struct{}{}:
						defer func() { <-r.downloadSem }()
					case <-ctx.Done():
						return
					}

					data, err := r.downloadMedia(ctx, api, m)
					if err != nil {
						r.logger.Warn().Err(err).Int("msg_id", int(rm.TGMessageID)).Msg("async download failed")
						return
					}
					if data == nil {
						return
					}

					rm.MediaData = data
					// Re-save with media data. The SQL ON CONFLICT will handle updating the media_data column.
					if err := r.database.SaveRawMessage(ctx, &rm); err != nil {
						r.logger.Error().Err(err).Int("msg_id", int(rm.TGMessageID)).Msg("failed to update raw message with media data")
					}
				}(msg.Media, *rawMsg)
			}

			// Start async link resolution
			links := linkextract.ExtractLinks(msg.Message)
			if len(links) > 0 && r.resolver != nil {
				go func(text string) {
					// We use a background context or a timeout context to not block the reader
					// but since it's a goroutine it's fine.
					// We don't really care about the results here, just populating the cache.
					_, _ = r.resolver.ResolveLinks(context.Background(), text, r.cfg.MaxLinksPerMessage, r.cfg.LinkCacheTTL, r.cfg.TelegramLinkCacheTTL)
				}(msg.Message)
			}

			// Extract channel discoveries asynchronously (only for new messages)
			go func(m *tg.Message, channelID string, channelPeerID int64, msgID int64, titles map[int64]string, accessHashes map[int64]int64) {
				// Check and mark if discoveries were already extracted for this message
				isNew, err := r.database.CheckAndMarkDiscoveriesExtracted(context.Background(), channelID, msgID)
				if err != nil {
					r.logger.Warn().Err(err).Msg("failed to check discoveries extracted flag")
					return
				}
				if !isNew {
					// Already extracted discoveries for this message
					return
				}

				discoveries := r.extractDiscoveries(m, channelID, channelPeerID, titles, accessHashes)
				for _, d := range discoveries {
					if err := r.database.RecordDiscovery(context.Background(), d); err != nil {
						r.logger.Warn().Err(err).Str("discovery", d.Username).Int64("peer_id", d.TGPeerID).Msg("failed to record discovery")
					}
				}
			}(msg, ch.ID, ch.TGPeerID, int64(msg.ID), channelTitles, channelAccessHashes)
		}
	}

	if count > 0 {
		r.logger.Info().Str("channel", ch.Username).Int("count", count).Msg("Saved messages for channel")
	} else {
		r.logger.Debug().Str("channel", ch.Username).Msg("No new messages for channel")
	}

	if maxID > ch.LastTGMessageID {
		if err := r.database.UpdateChannelLastMessageID(ctx, ch.ID, maxID); err != nil {
			r.logger.Error().Err(err).Str("channel", ch.Username).Int64("max_id", maxID).Msg("failed to update last message id")
		}
	}

	return count, nil
}

func (r *Reader) downloadMedia(ctx context.Context, api *tg.Client, media tg.MessageMediaClass) ([]byte, error) {
	var fileLocation tg.InputFileLocationClass

	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := m.Photo.(*tg.Photo)
		if !ok {
			return nil, nil
		}

		// Find the largest photo size
		var largest tg.PhotoSizeClass
		maxSize := 0
		for _, size := range photo.Sizes {
			switch s := size.(type) {
			case *tg.PhotoSize:
				if s.W*s.H > maxSize {
					maxSize = s.W * s.H
					largest = size
				}
			case *tg.PhotoSizeProgressive:
				if s.W*s.H > maxSize {
					maxSize = s.W * s.H
					largest = size
				}
			}
		}

		if largest == nil {
			return nil, nil
		}

		var thumbSize string
		switch s := largest.(type) {
		case *tg.PhotoSize:
			thumbSize = s.Type
		case *tg.PhotoSizeProgressive:
			thumbSize = s.Type
		default:
			return nil, nil
		}

		fileLocation = &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumbSize,
		}

	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			return nil, nil
		}

		// Check if it's an image
		isImage := strings.HasPrefix(doc.MimeType, "image/")
		if !isImage {
			// Check attributes for ImageSize
			for _, attr := range doc.Attributes {
				if _, ok := attr.(*tg.DocumentAttributeImageSize); ok {
					isImage = true
					break
				}
			}
		}

		if !isImage {
			return nil, nil
		}

		// Don't download huge files as "images" (limit to 10MB)
		if doc.Size > 10*1024*1024 {
			return nil, nil
		}

		fileLocation = &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		}

	default:
		return nil, nil
	}

	buf := new(bytes.Buffer)
	_, err := downloader.NewDownloader().Download(api, fileLocation).Stream(ctx, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}
	return buf.Bytes(), nil
}

// extractDiscoveries extracts channel discoveries from a message
// channelTitles is a map of channel IDs to titles from the API response
// channelAccessHashes is a map of channel IDs to access hashes from the API response
func (r *Reader) extractDiscoveries(msg *tg.Message, fromChannelID string, fromChannelPeerID int64, channelTitles map[int64]string, channelAccessHashes map[int64]int64) []db.Discovery {
	var discoveries []db.Discovery

	// Get engagement metrics for weighting
	views := msg.Views
	forwards := msg.Forwards

	// 1. Extract from forwards
	if fwd, ok := msg.GetFwdFrom(); ok {
		if fwd.FromID != nil {
			switch from := fwd.FromID.(type) {
			case *tg.PeerChannel:
				// Try to get title from forward header, then from API response chats
				title := fwd.FromName
				if title == "" {
					title = channelTitles[from.ChannelID]
				}
				if title == "" {
					r.logger.Debug().
						Int64("forward_from_id", from.ChannelID).
						Str("fwd_from_name", fwd.FromName).
						Int("chats_available", len(channelTitles)).
						Msg("Forward from channel without title in response")
				}
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      from.ChannelID,
					Title:         title,
					SourceType:    "forward",
					FromChannelID: fromChannelID,
					Views:         views,
					Forwards:      forwards,
					AccessHash:    channelAccessHashes[from.ChannelID],
				})
			}
		}
		// 1b. Extract from SavedFromPeer (messages forwarded from saved messages)
		if fwd.SavedFromPeer != nil {
			switch savedPeer := fwd.SavedFromPeer.(type) {
			case *tg.PeerChannel:
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      savedPeer.ChannelID,
					Title:         channelTitles[savedPeer.ChannelID],
					SourceType:    "saved_from_peer",
					FromChannelID: fromChannelID,
					Views:         views,
					Forwards:      forwards,
					AccessHash:    channelAccessHashes[savedPeer.ChannelID],
				})
			}
		}
	}

	// 2. Extract from reply-to chains (new strategy)
	if replyTo, ok := msg.GetReplyTo(); ok {
		if header, ok := replyTo.(*tg.MessageReplyHeader); ok {
			if header.ReplyToPeerID != nil {
				switch peer := header.ReplyToPeerID.(type) {
				case *tg.PeerChannel:
					// Only discover if it's a different channel
					if peer.ChannelID != fromChannelPeerID {
						discoveries = append(discoveries, db.Discovery{
							TGPeerID:      peer.ChannelID,
							Title:         channelTitles[peer.ChannelID],
							SourceType:    "reply",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
							AccessHash:    channelAccessHashes[peer.ChannelID],
						})
					}
				}
			}
		}
	}

	// 3. Extract from t.me links
	links := linkextract.ExtractLinks(msg.Message)
	for _, link := range links {
		if link.Type != linkextract.LinkTypeTelegram {
			continue
		}

		switch link.TelegramType {
		case "channel", "post":
			if link.Username != "" {
				discoveries = append(discoveries, db.Discovery{
					Username:      link.Username,
					SourceType:    "link",
					FromChannelID: fromChannelID,
					Views:         views,
					Forwards:      forwards,
				})
			} else if link.ChannelID != 0 {
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      link.ChannelID,
					SourceType:    "link",
					FromChannelID: fromChannelID,
					Views:         views,
					Forwards:      forwards,
				})
			}
		case "invite":
			discoveries = append(discoveries, db.Discovery{
				InviteLink:    link.URL,
				SourceType:    "link",
				FromChannelID: fromChannelID,
				Views:         views,
				Forwards:      forwards,
			})
		}
	}

	// 4. Extract from message entities (TextURL - hidden links)
	for _, entity := range msg.Entities {
		if textURL, ok := entity.(*tg.MessageEntityTextURL); ok {
			if strings.Contains(textURL.URL, "t.me/") {
				// Parse the hidden t.me link
				hiddenLinks := linkextract.ExtractLinks(textURL.URL)
				for _, link := range hiddenLinks {
					if link.Type != linkextract.LinkTypeTelegram {
						continue
					}
					switch link.TelegramType {
					case "channel", "post":
						if link.Username != "" {
							discoveries = append(discoveries, db.Discovery{
								Username:      link.Username,
								SourceType:    "entity_text_url",
								FromChannelID: fromChannelID,
								Views:         views,
								Forwards:      forwards,
							})
						} else if link.ChannelID != 0 {
							discoveries = append(discoveries, db.Discovery{
								TGPeerID:      link.ChannelID,
								SourceType:    "entity_text_url",
								FromChannelID: fromChannelID,
								Views:         views,
								Forwards:      forwards,
							})
						}
					case "invite":
						discoveries = append(discoveries, db.Discovery{
							InviteLink:    link.URL,
							SourceType:    "entity_text_url",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
						})
					}
				}
			}
		}
	}

	// 5. Extract @mentions
	mentions := linkextract.ExtractMentions(msg.Message)
	for _, username := range mentions {
		discoveries = append(discoveries, db.Discovery{
			Username:      username,
			SourceType:    "mention",
			FromChannelID: fromChannelID,
			Views:         views,
			Forwards:      forwards,
		})
	}

	// 6. Extract from inline keyboard buttons
	if msg.ReplyMarkup != nil {
		if inlineMarkup, ok := msg.ReplyMarkup.(*tg.ReplyInlineMarkup); ok {
			for _, row := range inlineMarkup.Rows {
				for _, btn := range row.Buttons {
					switch b := btn.(type) {
					case *tg.KeyboardButtonURL:
						if strings.Contains(b.URL, "t.me/") {
							btnLinks := linkextract.ExtractLinks(b.URL)
							for _, link := range btnLinks {
								if link.Type != linkextract.LinkTypeTelegram {
									continue
								}
								switch link.TelegramType {
								case "channel", "post":
									if link.Username != "" {
										discoveries = append(discoveries, db.Discovery{
											Username:      link.Username,
											SourceType:    "keyboard_url",
											FromChannelID: fromChannelID,
											Views:         views,
											Forwards:      forwards,
										})
									} else if link.ChannelID != 0 {
										discoveries = append(discoveries, db.Discovery{
											TGPeerID:      link.ChannelID,
											SourceType:    "keyboard_url",
											FromChannelID: fromChannelID,
											Views:         views,
											Forwards:      forwards,
										})
									}
								case "invite":
									discoveries = append(discoveries, db.Discovery{
										InviteLink:    link.URL,
										SourceType:    "keyboard_url",
										FromChannelID: fromChannelID,
										Views:         views,
										Forwards:      forwards,
									})
								}
							}
						}
					case *tg.KeyboardButtonWebView:
						if strings.Contains(b.URL, "t.me/") {
							btnLinks := linkextract.ExtractLinks(b.URL)
							for _, link := range btnLinks {
								if link.Type != linkextract.LinkTypeTelegram {
									continue
								}
								switch link.TelegramType {
								case "channel", "post":
									if link.Username != "" {
										discoveries = append(discoveries, db.Discovery{
											Username:      link.Username,
											SourceType:    "keyboard_url",
											FromChannelID: fromChannelID,
											Views:         views,
											Forwards:      forwards,
										})
									}
								case "invite":
									discoveries = append(discoveries, db.Discovery{
										InviteLink:    link.URL,
										SourceType:    "keyboard_url",
										FromChannelID: fromChannelID,
										Views:         views,
										Forwards:      forwards,
									})
								}
							}
						}
					case *tg.KeyboardButtonUserProfile:
						// User profile buttons reference a user ID (could be channel owner)
						discoveries = append(discoveries, db.Discovery{
							TGPeerID:      b.UserID,
							SourceType:    "user_profile_btn",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
						})
					case *tg.KeyboardButtonSwitchInline:
						// Switch inline buttons might contain @mentions in query text
						if b.Query != "" {
							queryMentions := linkextract.ExtractMentions(b.Query)
							for _, mention := range queryMentions {
								discoveries = append(discoveries, db.Discovery{
									Username:      mention,
									SourceType:    "switch_inline",
									FromChannelID: fromChannelID,
									Views:         views,
									Forwards:      forwards,
								})
							}
						}
					}
				}
			}
		}
	}

	// 7. Extract from web page media
	if msg.Media != nil {
		if webPageMedia, ok := msg.Media.(*tg.MessageMediaWebPage); ok {
			if webpage, ok := webPageMedia.Webpage.(*tg.WebPage); ok {
				// 7a. Extract from webpage URL
				if strings.Contains(webpage.URL, "t.me/") {
					wpLinks := linkextract.ExtractLinks(webpage.URL)
					for _, link := range wpLinks {
						if link.Type != linkextract.LinkTypeTelegram {
							continue
						}
						switch link.TelegramType {
						case "channel", "post":
							if link.Username != "" {
								discoveries = append(discoveries, db.Discovery{
									Username:      link.Username,
									SourceType:    "webpage_url",
									FromChannelID: fromChannelID,
									Views:         views,
									Forwards:      forwards,
								})
							} else if link.ChannelID != 0 {
								discoveries = append(discoveries, db.Discovery{
									TGPeerID:      link.ChannelID,
									SourceType:    "webpage_url",
									FromChannelID: fromChannelID,
									Views:         views,
									Forwards:      forwards,
								})
							}
						case "invite":
							discoveries = append(discoveries, db.Discovery{
								InviteLink:    link.URL,
								SourceType:    "webpage_url",
								FromChannelID: fromChannelID,
								Views:         views,
								Forwards:      forwards,
							})
						}
					}
				}
				// 7b. Extract from webpage author (might contain @username)
				if webpage.Author != "" {
					authorMentions := linkextract.ExtractMentions(webpage.Author)
					for _, mention := range authorMentions {
						discoveries = append(discoveries, db.Discovery{
							Username:      mention,
							SourceType:    "webpage_author",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
						})
					}
				}
				// 7c. Extract from embed URL (embedded content like YouTube, etc.)
				if webpage.EmbedURL != "" && strings.Contains(webpage.EmbedURL, "t.me/") {
					embedLinks := linkextract.ExtractLinks(webpage.EmbedURL)
					for _, link := range embedLinks {
						if link.Type != linkextract.LinkTypeTelegram {
							continue
						}
						switch link.TelegramType {
						case "channel", "post":
							if link.Username != "" {
								discoveries = append(discoveries, db.Discovery{
									Username:      link.Username,
									SourceType:    "embed_url",
									FromChannelID: fromChannelID,
									Views:         views,
									Forwards:      forwards,
								})
							}
						case "invite":
							discoveries = append(discoveries, db.Discovery{
								InviteLink:    link.URL,
								SourceType:    "embed_url",
								FromChannelID: fromChannelID,
								Views:         views,
								Forwards:      forwards,
							})
						}
					}
				}
				// 7d. Extract t.me links from webpage site name and description
				if webpage.SiteName != "" {
					siteLinks := linkextract.ExtractLinks(webpage.SiteName)
					for _, link := range siteLinks {
						if link.Type == linkextract.LinkTypeTelegram && link.Username != "" {
							discoveries = append(discoveries, db.Discovery{
								Username:      link.Username,
								SourceType:    "webpage_site",
								FromChannelID: fromChannelID,
								Views:         views,
								Forwards:      forwards,
							})
						}
					}
					siteMentions := linkextract.ExtractMentions(webpage.SiteName)
					for _, mention := range siteMentions {
						discoveries = append(discoveries, db.Discovery{
							Username:      mention,
							SourceType:    "webpage_site",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
						})
					}
				}
			}
		}
	}

	// 8. Extract from giveaway channels
	if msg.Media != nil {
		if giveaway, ok := msg.Media.(*tg.MessageMediaGiveaway); ok {
			for _, channelID := range giveaway.Channels {
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      channelID,
					Title:         channelTitles[channelID],
					SourceType:    "giveaway",
					FromChannelID: fromChannelID,
					Views:         views,
					Forwards:      forwards,
					AccessHash:    channelAccessHashes[channelID],
				})
			}
		}
	}

	// 9. Extract from story peer
	if msg.Media != nil {
		if storyMedia, ok := msg.Media.(*tg.MessageMediaStory); ok {
			if peer, ok := storyMedia.Peer.(*tg.PeerChannel); ok {
				if peer.ChannelID != fromChannelPeerID {
					discoveries = append(discoveries, db.Discovery{
						TGPeerID:      peer.ChannelID,
						Title:         channelTitles[peer.ChannelID],
						SourceType:    "story",
						FromChannelID: fromChannelID,
						Views:         views,
						Forwards:      forwards,
						AccessHash:    channelAccessHashes[peer.ChannelID],
					})
				}
			}
		}
	}

	// 10. Extract from poll voters
	if msg.Media != nil {
		if pollMedia, ok := msg.Media.(*tg.MessageMediaPoll); ok {
			if pollMedia.Results.RecentVoters != nil {
				for _, voter := range pollMedia.Results.RecentVoters {
					if peer, ok := voter.(*tg.PeerChannel); ok {
						if peer.ChannelID != fromChannelPeerID {
							discoveries = append(discoveries, db.Discovery{
								TGPeerID:      peer.ChannelID,
								Title:         channelTitles[peer.ChannelID],
								SourceType:    "poll_voter",
								FromChannelID: fromChannelID,
								Views:         views,
								Forwards:      forwards,
								AccessHash:    channelAccessHashes[peer.ChannelID],
							})
						}
					}
				}
			}
		}
	}

	// 11. Extract from reactions
	if reactions, ok := msg.GetReactions(); ok {
		for _, reaction := range reactions.RecentReactions {
			if peer, ok := reaction.PeerID.(*tg.PeerChannel); ok {
				if peer.ChannelID != fromChannelPeerID {
					discoveries = append(discoveries, db.Discovery{
						TGPeerID:      peer.ChannelID,
						Title:         channelTitles[peer.ChannelID],
						SourceType:    "reaction",
						FromChannelID: fromChannelID,
						Views:         views,
						Forwards:      forwards,
						AccessHash:    channelAccessHashes[peer.ChannelID],
					})
				}
			}
		}
	}

	// 12. Extract from MessageEntityMentionName (direct user ID references)
	for _, entity := range msg.Entities {
		switch e := entity.(type) {
		case *tg.MessageEntityMentionName:
			// Store as TGPeerID - these are user IDs that could be channel admins
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      e.UserID,
				SourceType:    "entity_mention_name",
				FromChannelID: fromChannelID,
				Views:         views,
				Forwards:      forwards,
			})
		case *tg.MessageEntityCustomEmoji:
			// Custom emoji document IDs can be traced to sticker pack channels
			// Store the document ID as a discovery hint (will need resolution)
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      e.DocumentID,
				SourceType:    "custom_emoji",
				FromChannelID: fromChannelID,
				Views:         views,
				Forwards:      forwards,
			})
		}
	}

	// 13. Extract from contact media
	if msg.Media != nil {
		if contact, ok := msg.Media.(*tg.MessageMediaContact); ok {
			if contact.UserID != 0 {
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      contact.UserID,
					SourceType:    "contact",
					FromChannelID: fromChannelID,
					Views:         views,
					Forwards:      forwards,
				})
			}
		}
	}

	// 14. Extract from via bot
	if msg.ViaBotID != 0 {
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      msg.ViaBotID,
			SourceType:    "via_bot",
			FromChannelID: fromChannelID,
			Views:         views,
			Forwards:      forwards,
		})
	}

	// 15. Extract from game media (games can reference bot developers)
	if msg.Media != nil {
		if game, ok := msg.Media.(*tg.MessageMediaGame); ok {
			// Game short name might contain channel references
			if game.Game.ShortName != "" {
				gameMentions := linkextract.ExtractMentions(game.Game.ShortName)
				for _, mention := range gameMentions {
					discoveries = append(discoveries, db.Discovery{
						Username:      mention,
						SourceType:    "game",
						FromChannelID: fromChannelID,
						Views:         views,
						Forwards:      forwards,
					})
				}
			}
			// Game description might contain links/mentions
			if game.Game.Description != "" {
				gameLinks := linkextract.ExtractLinks(game.Game.Description)
				for _, link := range gameLinks {
					if link.Type == linkextract.LinkTypeTelegram && link.Username != "" {
						discoveries = append(discoveries, db.Discovery{
							Username:      link.Username,
							SourceType:    "game",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
						})
					}
				}
				gameMentions := linkextract.ExtractMentions(game.Game.Description)
				for _, mention := range gameMentions {
					discoveries = append(discoveries, db.Discovery{
						Username:      mention,
						SourceType:    "game",
						FromChannelID: fromChannelID,
						Views:         views,
						Forwards:      forwards,
					})
				}
			}
		}
	}

	// 16. Extract from invoice media (payments can reference merchants)
	if msg.Media != nil {
		if invoice, ok := msg.Media.(*tg.MessageMediaInvoice); ok {
			// Invoice description might contain channel references
			if invoice.Description != "" {
				invoiceLinks := linkextract.ExtractLinks(invoice.Description)
				for _, link := range invoiceLinks {
					if link.Type == linkextract.LinkTypeTelegram && link.Username != "" {
						discoveries = append(discoveries, db.Discovery{
							Username:      link.Username,
							SourceType:    "invoice",
							FromChannelID: fromChannelID,
							Views:         views,
							Forwards:      forwards,
						})
					}
				}
				invoiceMentions := linkextract.ExtractMentions(invoice.Description)
				for _, mention := range invoiceMentions {
					discoveries = append(discoveries, db.Discovery{
						Username:      mention,
						SourceType:    "invoice",
						FromChannelID: fromChannelID,
						Views:         views,
						Forwards:      forwards,
					})
				}
			}
		}
	}

	return discoveries
}

// extractDiscoveriesFromService extracts channel discoveries from service messages
func (r *Reader) extractDiscoveriesFromService(msg *tg.MessageService, fromChannelID string) []db.Discovery {
	var discoveries []db.Discovery

	switch action := msg.Action.(type) {
	case *tg.MessageActionChatMigrateTo:
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      action.ChannelID,
			SourceType:    "migration",
			FromChannelID: fromChannelID,
		})
	case *tg.MessageActionChannelMigrateFrom:
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      action.ChatID,
			SourceType:    "migration",
			FromChannelID: fromChannelID,
		})
	case *tg.MessageActionChatAddUser:
		// Users added to chat - could be channel admins
		for _, userID := range action.Users {
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      userID,
				SourceType:    "chat_add_user",
				FromChannelID: fromChannelID,
			})
		}
	case *tg.MessageActionGiftCode:
		// Gift code might reference a boost peer (channel)
		if action.BoostPeer != nil {
			if peer, ok := action.BoostPeer.(*tg.PeerChannel); ok {
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      peer.ChannelID,
					SourceType:    "gift_code",
					FromChannelID: fromChannelID,
				})
			}
		}
	case *tg.MessageActionRequestedPeer:
		// Requested peer action contains selected peer IDs
		for _, peer := range action.Peers {
			switch p := peer.(type) {
			case *tg.PeerChannel:
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      p.ChannelID,
					SourceType:    "requested_peer",
					FromChannelID: fromChannelID,
				})
			case *tg.PeerUser:
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      p.UserID,
					SourceType:    "requested_peer",
					FromChannelID: fromChannelID,
				})
			}
		}
	case *tg.MessageActionGiveawayLaunch:
		// Giveaway launch - no direct channel refs in action, channels are in the media
	case *tg.MessageActionGiveawayResults:
		// Giveaway results only contain counts (WinnersCount, UnclaimedCount), not winner IDs
	case *tg.MessageActionTopicCreate:
		// Forum topic creation - parse title for mentions
		if action.Title != "" {
			mentions := linkextract.ExtractMentions(action.Title)
			for _, mention := range mentions {
				discoveries = append(discoveries, db.Discovery{
					Username:      mention,
					SourceType:    "topic_title",
					FromChannelID: fromChannelID,
				})
			}
		}
	case *tg.MessageActionTopicEdit:
		// Forum topic edit - parse new title for mentions
		if action.Title != "" {
			mentions := linkextract.ExtractMentions(action.Title)
			for _, mention := range mentions {
				discoveries = append(discoveries, db.Discovery{
					Username:      mention,
					SourceType:    "topic_title",
					FromChannelID: fromChannelID,
				})
			}
		}
	case *tg.MessageActionInviteToGroupCall:
		// Group call invitations contain user IDs
		for _, userID := range action.Users {
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      userID,
				SourceType:    "group_call_invite",
				FromChannelID: fromChannelID,
			})
		}
	case *tg.MessageActionChatJoinedByLink:
		// User joined via invite link - the inviter ID
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      action.InviterID,
			SourceType:    "invite_joiner",
			FromChannelID: fromChannelID,
		})
	case *tg.MessageActionChatCreate:
		// Chat creation - parse title for mentions
		if action.Title != "" {
			mentions := linkextract.ExtractMentions(action.Title)
			for _, mention := range mentions {
				discoveries = append(discoveries, db.Discovery{
					Username:      mention,
					SourceType:    "chat_title",
					FromChannelID: fromChannelID,
				})
			}
		}
	case *tg.MessageActionChannelCreate:
		// Channel creation - parse title for mentions
		if action.Title != "" {
			mentions := linkextract.ExtractMentions(action.Title)
			for _, mention := range mentions {
				discoveries = append(discoveries, db.Discovery{
					Username:      mention,
					SourceType:    "channel_title",
					FromChannelID: fromChannelID,
				})
			}
		}
	}

	return discoveries
}
