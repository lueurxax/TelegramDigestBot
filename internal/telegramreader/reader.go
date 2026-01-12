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

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/linkextract"
	"github.com/lueurxax/telegram-digest-bot/internal/linkresolver"
	"github.com/lueurxax/telegram-digest-bot/internal/observability"
)

// Constants for magic numbers and repeated strings.
const (
	// Concurrency limits
	concurrentDownloadsLimit = 5
	maxWorkerCount           = 10

	// Wait times in seconds
	errorWaitSeconds       = 10
	noChannelsWaitSeconds  = 30
	defaultCycleDelay      = 30
	activeCycleDelay       = 15
	resolutionSleepShortMs = 200
	resolutionSleepLongMs  = 500

	// Rate limiting
	millisecondsPerSecond = 1000
	jitterModulo          = 1000

	// Batch sizes
	unknownDiscoveryBatchSize    = 10
	inviteLinkDiscoveryBatchSize = 5

	// Slice capacities
	discoverySliceCapacity = 8
	webpageSliceCapacity   = 4

	// Log field names (goconst)
	logFieldChannels     = "channels"
	logFieldChannel      = "channel"
	logFieldCount        = "count"
	logFieldPeerID       = "peer_id"
	logFieldTitle        = "title"
	logFieldUsername     = "username"
	logFieldInviteLink   = "invite_link"
	logFieldMsgID        = "msg_id"

	// Error messages
	errMsgIncrementResolutionAttempts = "failed to increment resolution attempts"

	// Telegram link types (used in case statements for link.TelegramType)
	telegramLinkTypeChannel = "channel"
	telegramLinkTypePost    = "post"
	telegramLinkTypeInvite  = "invite"

	// Discovery source types
	sourceTypeKeyboardURL = "keyboard_url"
	sourceTypeGame        = "game"
	sourceTypeInvoice     = "invoice"
	sourceTypeWebpageSite = "webpage_site"
	sourceTypeTopicTitle  = "topic_title"

	// Error format strings
	errFmtWrapString = "%w: %s"
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

	if workerCount > maxWorkerCount {
		workerCount = maxWorkerCount // Cap at maxWorkerCount to avoid overwhelming Telegram API
	}

	return &Reader{
		cfg:         cfg,
		database:    database,
		logger:      logger,
		downloadSem: make(chan struct{}, concurrentDownloadsLimit), // limit concurrent downloads
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

type fetchResult struct {
	channel string
	count   int
	err     error
}

func (r *Reader) ingestMessages(ctx context.Context) error {
	api := tg.NewClient(r.client)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		channels, err := r.database.GetActiveChannels(ctx)
		if err != nil {
			r.logger.Error().Err(err).Msg("failed to get active channels")

			if err := r.wait(ctx, errorWaitSeconds*time.Second); err != nil {
				return err
			}

			continue
		}

		if len(channels) == 0 {
			r.logger.Info().Msg("No active channels to track. Waiting...")

			if err := r.wait(ctx, noChannelsWaitSeconds*time.Second); err != nil {
				return err
			}

			continue
		}

		r.logger.Info().Int(logFieldChannels, len(channels)).Msg("Starting ingestion cycle")

		start := time.Now()
		cycleMsgs := r.runIngestionCycle(ctx, api, channels)

		r.logger.Info().Int(logFieldChannels, len(channels)).Int("msgs", cycleMsgs).Dur("duration", time.Since(start)).Msg("Finished ingestion cycle")

		// Resolve unknown discoveries (channels with peer ID but no title)
		go r.resolveUnknownDiscoveries(ctx, api)

		// Resolve invite link discoveries
		go r.resolveInviteLinkDiscoveries(ctx, api)

		// Adaptive delay: shorter if we found messages, longer if quiet
		cycleDelay := defaultCycleDelay * time.Second
		if cycleMsgs > 0 {
			cycleDelay = activeCycleDelay * time.Second // Poll more frequently if active
		}

		if err := r.wait(ctx, cycleDelay); err != nil {
			return err
		}
	}
}

func (r *Reader) wait(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (r *Reader) runIngestionCycle(ctx context.Context, api *tg.Client, channels []db.Channel) int {
	// Calculate minimum delay between API calls based on RateLimitRPS
	minDelay := millisecondsPerSecond * time.Millisecond
	if r.cfg.RateLimitRPS > 0 {
		minDelay = time.Duration(millisecondsPerSecond/r.cfg.RateLimitRPS) * time.Millisecond
	}

	results := make(chan fetchResult, len(channels))

	// Process channels with worker pool
	for _, ch := range channels {
		// Acquire worker slot (blocks if all workers busy)
		select {
		case r.workerSem <- struct{}{}:
		case <-ctx.Done():
			results <- fetchResult{channel: ch.Username, err: ctx.Err()}
			continue
		}

		go func(ch db.Channel) {
			defer func() { <-r.workerSem }() // Release worker slot

			// Rate limiting delay with jitter
			jitter := minDelay + time.Duration(float64(minDelay)*0.5*(float64(time.Now().UnixNano()%jitterModulo)/float64(jitterModulo)))

			select {
			case <-ctx.Done():
				results <- fetchResult{channel: ch.Username, err: ctx.Err()}
				return
			case <-time.After(jitter):
			}

			msgs, err := r.fetchChannelMessages(ctx, api, ch)
			results <- fetchResult{channel: ch.Username, count: msgs, err: err}
		}(ch)
	}

	// Collect results
	cycleMsgs := 0

	for i := 0; i < len(channels); i++ {
		select {
		case <-ctx.Done():
			return cycleMsgs
		case result := <-results:
			if result.err != nil {
				r.logger.Error().Str(logFieldChannel, result.channel).Err(result.err).Msg("failed to fetch messages for channel")
			}

			cycleMsgs += result.count
		}
	}

	return cycleMsgs
}

// resolveUnknownDiscoveries attempts to fetch channel info for discoveries with peer IDs but no titles
func (r *Reader) resolveUnknownDiscoveries(ctx context.Context, api *tg.Client) {
	discoveries, err := r.database.GetDiscoveriesNeedingResolution(ctx, unknownDiscoveryBatchSize)
	if err != nil {
		r.logger.Warn().Err(err).Msg("failed to get discoveries needing resolution")
		return
	}

	if len(discoveries) == 0 {
		return
	}

	r.logger.Debug().Int(logFieldCount, len(discoveries)).Msg("Resolving unknown discoveries")

	for _, d := range discoveries {
		// Try to get channel info using InputChannel with peer ID and access hash
		channels, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
			&tg.InputChannel{
				ChannelID:  d.TGPeerID,
				AccessHash: d.AccessHash, // Use cached access hash if available
			},
		})
		if err != nil {
			r.logger.Debug().Err(err).Int64(logFieldPeerID, d.TGPeerID).Int64("access_hash", d.AccessHash).Msg("failed to resolve channel (may be private)")

			// Increment attempt counter so we don't keep trying forever
			if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
				r.logger.Warn().Err(err).Msg(errMsgIncrementResolutionAttempts)
			}

			time.Sleep(resolutionSleepShortMs * time.Millisecond)

			continue
		}

		// Extract channel info from response
		resolved := false

		if channelsResult, ok := channels.(*tg.MessagesChats); ok {
			for _, chat := range channelsResult.Chats {
				if channel, ok := chat.(*tg.Channel); ok && channel.ID == d.TGPeerID {
					r.logger.Info().
						Int64(logFieldPeerID, d.TGPeerID).
						Str(logFieldTitle, channel.Title).
						Str(logFieldUsername, channel.Username).
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
				r.logger.Warn().Err(err).Msg(errMsgIncrementResolutionAttempts)
			}
		}

		// Rate limit to avoid hitting Telegram API limits
		time.Sleep(resolutionSleepLongMs * time.Millisecond)
	}
}

// resolveInviteLinkDiscoveries attempts to fetch channel info for discoveries with invite links
func (r *Reader) resolveInviteLinkDiscoveries(ctx context.Context, api *tg.Client) {
	discoveries, err := r.database.GetInviteLinkDiscoveriesNeedingResolution(ctx, inviteLinkDiscoveryBatchSize)
	if err != nil {
		r.logger.Warn().Err(err).Msg("failed to get invite link discoveries needing resolution")

		return
	}

	if len(discoveries) == 0 {
		return
	}

	r.logger.Debug().Int(logFieldCount, len(discoveries)).Msg("Resolving invite link discoveries")

	for _, d := range discoveries {
		select {
		case <-ctx.Done():
			return
		default:
		}

		r.resolveSingleInviteLinkDiscovery(ctx, api, d)

		// Rate limit
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

func (r *Reader) resolveSingleInviteLinkDiscovery(ctx context.Context, api *tg.Client, d db.InviteLinkDiscovery) {
	// Extract hash from invite link (e.g., https://t.me/+abc123 -> abc123)
	hash := strings.TrimPrefix(d.InviteLink, "https://t.me/+")
	hash = strings.TrimPrefix(hash, "https://t.me/joinchat/")

	if hash == d.InviteLink || hash == "" {
		r.logger.Debug().Str(logFieldInviteLink, d.InviteLink).Msg("invalid invite link format")

		if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
			r.logger.Warn().Err(err).Msg(errMsgIncrementResolutionAttempts)
		}

		return
	}

	// Try to check the invite without joining
	invite, err := api.MessagesCheckChatInvite(ctx, hash)
	if err != nil {
		r.logger.Debug().Err(err).Str(logFieldInviteLink, d.InviteLink).Msg("failed to check invite link")

		if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
			r.logger.Warn().Err(err).Msg(errMsgIncrementResolutionAttempts)
		}

		return
	}

	// Extract channel info from invite response
	var (
		title, username    string
		peerID, accessHash int64
	)

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
			Str(logFieldTitle, title).
			Str(logFieldUsername, username).
			Int64(logFieldPeerID, peerID).
			Msg("Resolved invite link discovery")

		if err := r.database.UpdateDiscoveryFromInvite(ctx, d.ID, title, username, peerID, accessHash); err != nil {
			r.logger.Warn().Err(err).Msg("failed to update discovery from invite")
		}
	} else {
		// Couldn't extract info
		if err := r.database.IncrementDiscoveryResolutionAttempts(ctx, d.ID); err != nil {
			r.logger.Warn().Err(err).Msg(errMsgIncrementResolutionAttempts)
		}
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
			case telegramLinkTypeChannel, telegramLinkTypePost:
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
			case telegramLinkTypeInvite:
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
	r.logger.Debug().Str(logFieldUsername, ch.Username).Str(logFieldTitle, ch.Title).Msg("Fetching messages for channel")

	if err := r.ensureJoined(ctx, api, &ch); err != nil {
		return 0, err
	}

	peer, err := r.resolvePeer(ctx, api, &ch)
	if err != nil {
		return 0, err
	}

	history, err := r.fetchHistory(ctx, api, peer, ch)
	if err != nil {
		return 0, err
	}

	if history == nil {
		return 0, nil
	}

	return r.processHistoryMessages(ctx, api, history, ch)
}

func (r *Reader) ensureJoined(ctx context.Context, api *tg.Client, ch *db.Channel) error {
	// 1. Join by invite link if needed
	if ch.InviteLink == "" || ch.TGPeerID != 0 {
		return nil
	}

	hash := ch.InviteLink

	for _, prefix := range []string{"https://t.me/joinchat/", "https://t.me/+", "t.me/joinchat/", "t.me/+"} {
		if strings.HasPrefix(hash, prefix) {
			hash = strings.TrimPrefix(hash, prefix)

			break
		}
	}

	r.logger.Info().Str(logFieldInviteLink, ch.InviteLink).Msg("Attempting to join channel by invite link")

	updates, err := api.MessagesImportChatInvite(ctx, hash)
	if err != nil {
		if !tgerr.Is(err, "USER_ALREADY_PARTICIPANT") {
			return fmt.Errorf("failed to join channel by invite link: %w", err)
		}

		// If already joined, check invite to get info
		invite, err := api.MessagesCheckChatInvite(ctx, hash)
		if err != nil {
			return fmt.Errorf("failed to check chat invite: %w", err)
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
				go r.extractDiscoveriesFromChannelFull(ctx, api, ch.ID, ch.TGPeerID, ch.AccessHash)
			}
		default:
			return fmt.Errorf("%w: %T", ErrUnexpectedInviteType, invite)
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
					go r.extractDiscoveriesFromChannelFull(ctx, api, ch.ID, ch.TGPeerID, ch.AccessHash)
				}
			}
		}
	}

	return nil
}

func (r *Reader) resolvePeer(ctx context.Context, api *tg.Client, ch *db.Channel) (tg.InputPeerClass, error) {
	// Resolve peer - OPTIMIZATION: Skip API call if we already have valid peer info
	var (
		peer        tg.InputPeerClass
		resolvedNow bool
	)

	if ch.TGPeerID != 0 && ch.AccessHash != 0 {
		// We already have valid peer info, use it directly (skip API call)
		peer = &tg.InputPeerChannel{
			ChannelID:  ch.TGPeerID,
			AccessHash: ch.AccessHash,
		}

		r.logger.Debug().Str(logFieldUsername, ch.Username).Int64(logFieldPeerID, ch.TGPeerID).Msg("Using cached peer info")
	} else if ch.Username != "" {
		// Need to resolve username to get peer info
		r.logger.Debug().Str(logFieldUsername, ch.Username).Msg("Resolving username (no cached peer info)")

		resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: ch.Username})
		if err != nil {
			return nil, fmt.Errorf("failed to resolve username: %w", err)
		}

		if len(resolved.Chats) == 0 {
			return nil, fmt.Errorf(errFmtWrapString, ErrChannelNotFound, ch.Username)
		}

		channel, ok := resolved.Chats[0].(*tg.Channel)
		if !ok {
			return nil, fmt.Errorf(errFmtWrapString, ErrNotAChannel, ch.Username)
		}

		// Update channel info
		r.logger.Info().Str(logFieldUsername, ch.Username).Int64(logFieldPeerID, channel.ID).Str(logFieldTitle, channel.Title).Msg("Caching channel info")

		ch.TGPeerID = channel.ID
		ch.AccessHash = channel.AccessHash
		ch.Title = channel.Title
		resolvedNow = true
		peer = &tg.InputPeerChannel{
			ChannelID:  ch.TGPeerID,
			AccessHash: ch.AccessHash,
		}
	} else if ch.TGPeerID != 0 {
		return nil, fmt.Errorf("%w: %d", ErrMissingAccessHash, ch.TGPeerID)
	} else {
		return nil, fmt.Errorf(errFmtWrapString, ErrNoChannelIdentifier, ch.ID)
	}

	// Update channel info in DB if it was just resolved OR if description is missing
	if resolvedNow || (ch.Description == "" && ch.TGPeerID != 0 && ch.AccessHash != 0) {
		description := ch.Description

		if description == "" {
			r.logger.Info().Int64(logFieldPeerID, ch.TGPeerID).Msg("Fetching missing channel description")

			var err error

			description, err = r.fetchChannelDescription(ctx, api, ch.TGPeerID, ch.AccessHash)
			if err != nil {
				r.logger.Warn().Err(err).Int64(logFieldPeerID, ch.TGPeerID).Msg("failed to fetch channel description")
			}
		}

		// Only update if we resolved ID/Hash OR if we actually got a description
		if resolvedNow || (description != "" && ch.Description == "") {
			if err := r.database.UpdateChannel(ctx, ch.ID, ch.TGPeerID, ch.Title, ch.AccessHash, ch.Username, description); err != nil {
				r.logger.Error().Err(err).Msg("failed to update channel info")
			}

			ch.Description = description

			// Extract discoveries from channel description and linked chat (async)
			go r.extractDiscoveriesFromChannelFull(ctx, api, ch.ID, ch.TGPeerID, ch.AccessHash)
		}
	}

	return peer, nil
}

func (r *Reader) fetchHistory(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, ch db.Channel) (tg.MessagesMessagesClass, error) {
	r.logger.Debug().Str(logFieldUsername, ch.Username).Int64(logFieldPeerID, ch.TGPeerID).Int64("last_id", ch.LastTGMessageID).Msg("Getting history")

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
			r.logger.Warn().Int("seconds", floodErr.Argument).Str(logFieldChannel, ch.Username).Msg("flood wait")

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(floodErr.Argument) * time.Second):
			}

			// Retry after flood wait
			return r.fetchHistory(ctx, api, peer, ch)
		}

		return nil, fmt.Errorf("failed to get history: %w", err)
	}

	return history, nil
}

type historyProcessingContext struct {
	api                  *tg.Client
	ch                   db.Channel
	channelTitles        map[int64]string
	channelAccessHashes  map[int64]int64
}

func (r *Reader) extractHistoryData(history tg.MessagesMessagesClass) ([]tg.MessageClass, []tg.ChatClass, bool) {
	switch h := history.(type) {
	case *tg.MessagesMessages:
		return h.Messages, h.Chats, true
	case *tg.MessagesMessagesSlice:
		return h.Messages, h.Chats, true
	case *tg.MessagesChannelMessages:
		return h.Messages, h.Chats, true
	case *tg.MessagesMessagesNotModified:
		return nil, nil, false
	}

	return nil, nil, false
}

func (r *Reader) buildChannelLookups(chats []tg.ChatClass) (map[int64]string, map[int64]int64) {
	titles := make(map[int64]string)
	accessHashes := make(map[int64]int64)

	for _, chat := range chats {
		if channel, ok := chat.(*tg.Channel); ok {
			titles[channel.ID] = channel.Title
			accessHashes[channel.ID] = channel.AccessHash
		}
	}

	return titles, accessHashes
}

func (r *Reader) processSingleMessage(ctx context.Context, hpc *historyProcessingContext, msg *tg.Message) bool {
	if msg.Message == "" && msg.Media == nil {
		return false
	}

	entitiesJSON, _ := json.Marshal(msg.Entities)
	mediaJSON, _ := json.Marshal(msg.Media)
	_, isForward := msg.GetFwdFrom()

	rawMsg := &db.RawMessage{
		ChannelID:     hpc.ch.ID,
		TGMessageID:   int64(msg.ID),
		TGDate:        time.Unix(int64(msg.Date), 0),
		Text:          msg.Message,
		EntitiesJSON:  entitiesJSON,
		MediaJSON:     mediaJSON,
		CanonicalHash: r.canonicalize(msg.Message),
		IsForward:     isForward,
	}

	if err := r.database.SaveRawMessage(ctx, rawMsg); err != nil {
		r.logger.Error().Err(err).Str(logFieldChannel, hpc.ch.Username).Int(logFieldMsgID, msg.ID).Msg("failed to save raw message")

		return false
	}

	observability.MessagesIngested.WithLabelValues(hpc.ch.Username).Inc()
	r.startAsyncMediaDownload(ctx, hpc, msg, rawMsg)
	r.startAsyncLinkResolution(ctx, msg.Message)
	r.startAsyncDiscoveryExtraction(ctx, hpc, msg)

	return true
}

func (r *Reader) startAsyncMediaDownload(ctx context.Context, hpc *historyProcessingContext, msg *tg.Message, rawMsg *db.RawMessage) {
	if msg.Media == nil {
		return
	}

	go func(ctx context.Context, api *tg.Client, m tg.MessageMediaClass, rm db.RawMessage) {
		select {
		case r.downloadSem <- struct{}{}:
			defer func() { <-r.downloadSem }()
		case <-ctx.Done():
			return
		}

		data, err := r.downloadMedia(ctx, api, m)
		if err != nil {
			r.logger.Warn().Err(err).Int(logFieldMsgID, int(rm.TGMessageID)).Msg("async download failed")

			return
		}

		if data == nil {
			return
		}

		rm.MediaData = data

		if err := r.database.SaveRawMessage(ctx, &rm); err != nil {
			r.logger.Error().Err(err).Int(logFieldMsgID, int(rm.TGMessageID)).Msg("failed to update raw message with media data")
		}
	}(ctx, hpc.api, msg.Media, *rawMsg)
}

func (r *Reader) startAsyncLinkResolution(ctx context.Context, text string) {
	links := linkextract.ExtractLinks(text)
	if len(links) == 0 || r.resolver == nil {
		return
	}

	go func(ctx context.Context, text string) {
		_, _ = r.resolver.ResolveLinks(ctx, text, r.cfg.MaxLinksPerMessage, r.cfg.LinkCacheTTL, r.cfg.TelegramLinkCacheTTL)
	}(ctx, text)
}

func (r *Reader) startAsyncDiscoveryExtraction(ctx context.Context, hpc *historyProcessingContext, msg *tg.Message) {
	go func(ctx context.Context, m *tg.Message, channelID string, channelPeerID int64, msgID int64, titles map[int64]string, accessHashes map[int64]int64) {
		isNew, err := r.database.CheckAndMarkDiscoveriesExtracted(ctx, channelID, msgID)
		if err != nil {
			r.logger.Warn().Err(err).Msg("failed to check discoveries extracted flag")

			return
		}

		if !isNew {
			return
		}

		discoveries := r.extractDiscoveries(m, channelID, channelPeerID, titles, accessHashes)
		for _, d := range discoveries {
			if err := r.database.RecordDiscovery(ctx, d); err != nil {
				r.logger.Warn().Err(err).Str("discovery", d.Username).Int64(logFieldPeerID, d.TGPeerID).Msg("failed to record discovery")
			}
		}
	}(ctx, msg, hpc.ch.ID, hpc.ch.TGPeerID, int64(msg.ID), hpc.channelTitles, hpc.channelAccessHashes)
}

func (r *Reader) processHistoryMessages(ctx context.Context, api *tg.Client, history tg.MessagesMessagesClass, ch db.Channel) (int, error) {
	messages, chats, ok := r.extractHistoryData(history)
	if !ok {
		r.logger.Debug().Str(logFieldChannel, ch.Username).Msg("History not modified")

		return 0, nil
	}

	hpc := &historyProcessingContext{
		api: api,
		ch:  ch,
	}
	hpc.channelTitles, hpc.channelAccessHashes = r.buildChannelLookups(chats)

	r.logger.Debug().Str(logFieldChannel, ch.Username).Int(logFieldCount, len(messages)).Int("chats_in_response", len(chats)).Msg("Processing messages")

	count := 0
	maxID := ch.LastTGMessageID

	for _, m := range messages {
		if svcMsg, ok := m.(*tg.MessageService); ok {
			r.processServiceMessage(ctx, svcMsg, ch.ID)

			continue
		}

		msg, ok := m.(*tg.Message)
		if !ok {
			continue
		}

		if msg.ID > int(maxID) {
			maxID = int64(msg.ID)
		}

		if r.processSingleMessage(ctx, hpc, msg) {
			count++
		}
	}

	r.logProcessingResult(ctx, ch, count, maxID)

	return count, nil
}

func (r *Reader) processServiceMessage(ctx context.Context, svcMsg *tg.MessageService, channelID string) {
	go func(ctx context.Context, sm *tg.MessageService, chID string) {
		discoveries := r.extractDiscoveriesFromService(sm, chID)
		for _, d := range discoveries {
			if err := r.database.RecordDiscovery(ctx, d); err != nil {
				r.logger.Warn().Err(err).Int64(logFieldPeerID, d.TGPeerID).Msg("failed to record service message discovery")
			}
		}
	}(ctx, svcMsg, channelID)
}

func (r *Reader) logProcessingResult(ctx context.Context, ch db.Channel, count int, maxID int64) {
	if count > 0 {
		r.logger.Info().Str(logFieldChannel, ch.Username).Int(logFieldCount, count).Msg("Saved messages for channel")
	} else {
		r.logger.Debug().Str(logFieldChannel, ch.Username).Msg("No new messages for channel")
	}

	if maxID > ch.LastTGMessageID {
		if err := r.database.UpdateChannelLastMessageID(ctx, ch.ID, maxID); err != nil {
			r.logger.Error().Err(err).Str(logFieldChannel, ch.Username).Int64("max_id", maxID).Msg("failed to update last message id")
		}
	}
}

func (r *Reader) downloadMedia(ctx context.Context, api *tg.Client, media tg.MessageMediaClass) ([]byte, error) {
	var fileLocation tg.InputFileLocationClass

	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		fileLocation = r.getPhotoFileLocation(m)
	case *tg.MessageMediaDocument:
		fileLocation = r.getDocumentFileLocation(m)
	default:
		return nil, nil
	}

	if fileLocation == nil {
		return nil, nil
	}

	buf := new(bytes.Buffer)

	_, err := downloader.NewDownloader().Download(api, fileLocation).Stream(ctx, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}

	return buf.Bytes(), nil
}

func (r *Reader) getPhotoFileLocation(m *tg.MessageMediaPhoto) tg.InputFileLocationClass {
	photo, ok := m.Photo.(*tg.Photo)
	if !ok {
		return nil
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
		return nil
	}

	var thumbSize string

	switch s := largest.(type) {
	case *tg.PhotoSize:
		thumbSize = s.Type
	case *tg.PhotoSizeProgressive:
		thumbSize = s.Type
	default:
		return nil
	}

	return &tg.InputPhotoFileLocation{
		ID:            photo.ID,
		AccessHash:    photo.AccessHash,
		FileReference: photo.FileReference,
		ThumbSize:     thumbSize,
	}
}

func (r *Reader) getDocumentFileLocation(m *tg.MessageMediaDocument) tg.InputFileLocationClass {
	doc, ok := m.Document.(*tg.Document)
	if !ok {
		return nil
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
		return nil
	}

	// Don't download huge files as "images" (limit to 10MB)
	if doc.Size > 10*1024*1024 {
		return nil
	}

	return &tg.InputDocumentFileLocation{
		ID:            doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
	}
}

// discoveryContext holds common parameters for discovery extraction
type discoveryContext struct {
	fromChannelID       string
	fromChannelPeerID   int64
	channelTitles       map[int64]string
	channelAccessHashes map[int64]int64
	views               int
	forwards            int
}

// extractTelegramLinkDiscoveries extracts discoveries from telegram links with the given source type
func (dc *discoveryContext) extractTelegramLinkDiscoveries(url, sourceType string) []db.Discovery {
	if !strings.Contains(url, "t.me/") {
		return nil
	}

	var discoveries []db.Discovery

	links := linkextract.ExtractLinks(url)
	for _, link := range links {
		if link.Type != linkextract.LinkTypeTelegram {
			continue
		}

		switch link.TelegramType {
		case telegramLinkTypeChannel, telegramLinkTypePost:
			if link.Username != "" {
				discoveries = append(discoveries, db.Discovery{
					Username:      link.Username,
					SourceType:    sourceType,
					FromChannelID: dc.fromChannelID,
					Views:         dc.views,
					Forwards:      dc.forwards,
				})
			} else if link.ChannelID != 0 {
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      link.ChannelID,
					SourceType:    sourceType,
					FromChannelID: dc.fromChannelID,
					Views:         dc.views,
					Forwards:      dc.forwards,
				})
			}
		case telegramLinkTypeInvite:
			discoveries = append(discoveries, db.Discovery{
				InviteLink:    link.URL,
				SourceType:    sourceType,
				FromChannelID: dc.fromChannelID,
				Views:         dc.views,
				Forwards:      dc.forwards,
			})
		}
	}

	return discoveries
}

// extractMentionDiscoveries extracts discoveries from @mentions in text
func (dc *discoveryContext) extractMentionDiscoveries(text, sourceType string) []db.Discovery {
	mentions := linkextract.ExtractMentions(text)
	if len(mentions) == 0 {
		return nil
	}

	discoveries := make([]db.Discovery, 0, len(mentions))
	for _, username := range mentions {
		discoveries = append(discoveries, db.Discovery{
			Username:      username,
			SourceType:    sourceType,
			FromChannelID: dc.fromChannelID,
			Views:         dc.views,
			Forwards:      dc.forwards,
		})
	}

	return discoveries
}

// newChannelDiscovery creates a discovery for a channel peer ID
func (dc *discoveryContext) newChannelDiscovery(peerID int64, sourceType string) db.Discovery {
	return db.Discovery{
		TGPeerID:      peerID,
		Title:         dc.channelTitles[peerID],
		SourceType:    sourceType,
		FromChannelID: dc.fromChannelID,
		Views:         dc.views,
		Forwards:      dc.forwards,
		AccessHash:    dc.channelAccessHashes[peerID],
	}
}

// extractDiscoveries extracts channel discoveries from a message
func (r *Reader) extractDiscoveries(msg *tg.Message, fromChannelID string, fromChannelPeerID int64, channelTitles map[int64]string, channelAccessHashes map[int64]int64) []db.Discovery {
	dc := &discoveryContext{
		fromChannelID:       fromChannelID,
		fromChannelPeerID:   fromChannelPeerID,
		channelTitles:       channelTitles,
		channelAccessHashes: channelAccessHashes,
		views:               msg.Views,
		forwards:            msg.Forwards,
	}

	// Preallocate with reasonable initial capacity for typical message
	discoveries := make([]db.Discovery, 0, discoverySliceCapacity)

	// 1-2. Extract from forwards and replies
	discoveries = append(discoveries, r.extractFromForwards(msg, fromChannelID, channelTitles, channelAccessHashes, dc.views, dc.forwards)...)
	discoveries = append(discoveries, r.extractFromReplies(msg, fromChannelID, fromChannelPeerID, channelTitles, channelAccessHashes, dc.views, dc.forwards)...)

	// 3. Extract from t.me links in message text
	discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(msg.Message, "link")...)

	// 4. Extract from message entities (TextURL - hidden links)
	discoveries = append(discoveries, r.extractFromEntities(msg.Entities, dc)...)

	// 5. Extract @mentions
	discoveries = append(discoveries, dc.extractMentionDiscoveries(msg.Message, "mention")...)

	// 6. Extract from inline keyboard buttons
	discoveries = append(discoveries, r.extractFromKeyboard(msg.ReplyMarkup, dc)...)

	// 7-16. Extract from media
	discoveries = append(discoveries, r.extractFromMedia(msg, dc)...)

	return discoveries
}

func (r *Reader) extractFromEntities(entities []tg.MessageEntityClass, dc *discoveryContext) []db.Discovery {
	var discoveries []db.Discovery

	for _, entity := range entities {
		switch e := entity.(type) {
		case *tg.MessageEntityTextURL:
			discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(e.URL, "entity_text_url")...)
		case *tg.MessageEntityMentionName:
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      e.UserID,
				SourceType:    "entity_mention_name",
				FromChannelID: dc.fromChannelID,
				Views:         dc.views,
				Forwards:      dc.forwards,
			})
		case *tg.MessageEntityCustomEmoji:
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      e.DocumentID,
				SourceType:    "custom_emoji",
				FromChannelID: dc.fromChannelID,
				Views:         dc.views,
				Forwards:      dc.forwards,
			})
		}
	}

	return discoveries
}

func (r *Reader) extractFromKeyboard(markup tg.ReplyMarkupClass, dc *discoveryContext) []db.Discovery {
	if markup == nil {
		return nil
	}

	inlineMarkup, ok := markup.(*tg.ReplyInlineMarkup)
	if !ok {
		return nil
	}

	var discoveries []db.Discovery

	for _, row := range inlineMarkup.Rows {
		for _, btn := range row.Buttons {
			switch b := btn.(type) {
			case *tg.KeyboardButtonURL:
				discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(b.URL, sourceTypeKeyboardURL)...)
			case *tg.KeyboardButtonWebView:
				discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(b.URL, sourceTypeKeyboardURL)...)
			case *tg.KeyboardButtonUserProfile:
				discoveries = append(discoveries, db.Discovery{
					TGPeerID:      b.UserID,
					SourceType:    "user_profile_btn",
					FromChannelID: dc.fromChannelID,
					Views:         dc.views,
					Forwards:      dc.forwards,
				})
			case *tg.KeyboardButtonSwitchInline:
				discoveries = append(discoveries, dc.extractMentionDiscoveries(b.Query, "switch_inline")...)
			}
		}
	}

	return discoveries
}

func (r *Reader) extractFromMedia(msg *tg.Message, dc *discoveryContext) []db.Discovery {
	var discoveries []db.Discovery

	if msg.Media == nil {
		// Extract from reactions even without media
		discoveries = append(discoveries, r.extractFromReactions(msg, dc)...)

		// Extract from via bot
		if msg.ViaBotID != 0 {
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      msg.ViaBotID,
				SourceType:    "via_bot",
				FromChannelID: dc.fromChannelID,
				Views:         dc.views,
				Forwards:      dc.forwards,
			})
		}

		return discoveries
	}

	switch m := msg.Media.(type) {
	case *tg.MessageMediaWebPage:
		discoveries = append(discoveries, r.extractFromWebPage(m, dc)...)
	case *tg.MessageMediaGiveaway:
		for _, channelID := range m.Channels {
			discoveries = append(discoveries, dc.newChannelDiscovery(channelID, "giveaway"))
		}
	case *tg.MessageMediaStory:
		if peer, ok := m.Peer.(*tg.PeerChannel); ok && peer.ChannelID != dc.fromChannelPeerID {
			discoveries = append(discoveries, dc.newChannelDiscovery(peer.ChannelID, "story"))
		}
	case *tg.MessageMediaPoll:
		discoveries = append(discoveries, r.extractFromPoll(m, dc)...)
	case *tg.MessageMediaContact:
		if m.UserID != 0 {
			discoveries = append(discoveries, db.Discovery{
				TGPeerID:      m.UserID,
				SourceType:    "contact",
				FromChannelID: dc.fromChannelID,
				Views:         dc.views,
				Forwards:      dc.forwards,
			})
		}
	case *tg.MessageMediaGame:
		discoveries = append(discoveries, dc.extractMentionDiscoveries(m.Game.ShortName, sourceTypeGame)...)
		discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(m.Game.Description, sourceTypeGame)...)
		discoveries = append(discoveries, dc.extractMentionDiscoveries(m.Game.Description, sourceTypeGame)...)
	case *tg.MessageMediaInvoice:
		discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(m.Description, sourceTypeInvoice)...)
		discoveries = append(discoveries, dc.extractMentionDiscoveries(m.Description, sourceTypeInvoice)...)
	}

	// Extract from reactions
	discoveries = append(discoveries, r.extractFromReactions(msg, dc)...)

	// Extract from via bot
	if msg.ViaBotID != 0 {
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      msg.ViaBotID,
			SourceType:    "via_bot",
			FromChannelID: dc.fromChannelID,
			Views:         dc.views,
			Forwards:      dc.forwards,
		})
	}

	return discoveries
}

func (r *Reader) extractFromWebPage(media *tg.MessageMediaWebPage, dc *discoveryContext) []db.Discovery {
	webpage, ok := media.Webpage.(*tg.WebPage)
	if !ok {
		return nil
	}

	// Preallocate with reasonable initial capacity for typical webpage
	discoveries := make([]db.Discovery, 0, webpageSliceCapacity)

	discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(webpage.URL, "webpage_url")...)
	discoveries = append(discoveries, dc.extractMentionDiscoveries(webpage.Author, "webpage_author")...)
	discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(webpage.EmbedURL, "embed_url")...)
	discoveries = append(discoveries, dc.extractTelegramLinkDiscoveries(webpage.SiteName, sourceTypeWebpageSite)...)
	discoveries = append(discoveries, dc.extractMentionDiscoveries(webpage.SiteName, sourceTypeWebpageSite)...)

	return discoveries
}

func (r *Reader) extractFromPoll(media *tg.MessageMediaPoll, dc *discoveryContext) []db.Discovery {
	if media.Results.RecentVoters == nil {
		return nil
	}

	var discoveries []db.Discovery

	for _, voter := range media.Results.RecentVoters {
		if peer, ok := voter.(*tg.PeerChannel); ok && peer.ChannelID != dc.fromChannelPeerID {
			discoveries = append(discoveries, dc.newChannelDiscovery(peer.ChannelID, "poll_voter"))
		}
	}

	return discoveries
}

func (r *Reader) extractFromReactions(msg *tg.Message, dc *discoveryContext) []db.Discovery {
	reactions, ok := msg.GetReactions()
	if !ok {
		return nil
	}

	var discoveries []db.Discovery

	for _, reaction := range reactions.RecentReactions {
		if peer, ok := reaction.PeerID.(*tg.PeerChannel); ok && peer.ChannelID != dc.fromChannelPeerID {
			discoveries = append(discoveries, dc.newChannelDiscovery(peer.ChannelID, "reaction"))
		}
	}

	return discoveries
}

func (r *Reader) extractFromForwards(msg *tg.Message, fromChannelID string, channelTitles map[int64]string, channelAccessHashes map[int64]int64, views, forwards int) []db.Discovery {
	var discoveries []db.Discovery

	if fwd, ok := msg.GetFwdFrom(); ok {
		if fwd.FromID != nil {
			switch from := fwd.FromID.(type) {
			case *tg.PeerChannel:
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

	return discoveries
}

func (r *Reader) extractFromReplies(msg *tg.Message, fromChannelID string, fromChannelPeerID int64, channelTitles map[int64]string, channelAccessHashes map[int64]int64, views, forwards int) []db.Discovery {
	var discoveries []db.Discovery

	if replyTo, ok := msg.GetReplyTo(); ok {
		if header, ok := replyTo.(*tg.MessageReplyHeader); ok {
			if header.ReplyToPeerID != nil {
				switch peer := header.ReplyToPeerID.(type) {
				case *tg.PeerChannel:
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

	return discoveries
}

// extractDiscoveriesFromService extracts channel discoveries from service messages
func extractServiceMentions(title, sourceType, fromChannelID string) []db.Discovery {
	if title == "" {
		return nil
	}

	var discoveries []db.Discovery

	for _, mention := range linkextract.ExtractMentions(title) {
		discoveries = append(discoveries, db.Discovery{
			Username:      mention,
			SourceType:    sourceType,
			FromChannelID: fromChannelID,
		})
	}

	return discoveries
}

func extractServiceUserIDs(userIDs []int64, sourceType, fromChannelID string) []db.Discovery {
	if len(userIDs) == 0 {
		return nil
	}

	discoveries := make([]db.Discovery, 0, len(userIDs))
	for _, userID := range userIDs {
		discoveries = append(discoveries, db.Discovery{
			TGPeerID:      userID,
			SourceType:    sourceType,
			FromChannelID: fromChannelID,
		})
	}

	return discoveries
}

func (r *Reader) extractDiscoveriesFromService(msg *tg.MessageService, fromChannelID string) []db.Discovery {
	var discoveries []db.Discovery

	switch action := msg.Action.(type) {
	case *tg.MessageActionChatMigrateTo:
		discoveries = append(discoveries, db.Discovery{TGPeerID: action.ChannelID, SourceType: "migration", FromChannelID: fromChannelID})
	case *tg.MessageActionChannelMigrateFrom:
		discoveries = append(discoveries, db.Discovery{TGPeerID: action.ChatID, SourceType: "migration", FromChannelID: fromChannelID})
	case *tg.MessageActionChatAddUser:
		discoveries = append(discoveries, extractServiceUserIDs(action.Users, "chat_add_user", fromChannelID)...)
	case *tg.MessageActionGiftCode:
		if action.BoostPeer != nil {
			if peer, ok := action.BoostPeer.(*tg.PeerChannel); ok {
				discoveries = append(discoveries, db.Discovery{TGPeerID: peer.ChannelID, SourceType: "gift_code", FromChannelID: fromChannelID})
			}
		}
	case *tg.MessageActionRequestedPeer:
		discoveries = append(discoveries, r.extractFromRequestedPeers(action.Peers, fromChannelID)...)
	case *tg.MessageActionTopicCreate:
		discoveries = append(discoveries, extractServiceMentions(action.Title, sourceTypeTopicTitle, fromChannelID)...)
	case *tg.MessageActionTopicEdit:
		discoveries = append(discoveries, extractServiceMentions(action.Title, sourceTypeTopicTitle, fromChannelID)...)
	case *tg.MessageActionInviteToGroupCall:
		discoveries = append(discoveries, extractServiceUserIDs(action.Users, "group_call_invite", fromChannelID)...)
	case *tg.MessageActionChatJoinedByLink:
		discoveries = append(discoveries, db.Discovery{TGPeerID: action.InviterID, SourceType: "invite_joiner", FromChannelID: fromChannelID})
	case *tg.MessageActionChatCreate:
		discoveries = append(discoveries, extractServiceMentions(action.Title, "chat_title", fromChannelID)...)
	case *tg.MessageActionChannelCreate:
		discoveries = append(discoveries, extractServiceMentions(action.Title, "channel_title", fromChannelID)...)
	}

	return discoveries
}

func (r *Reader) extractFromRequestedPeers(peers []tg.PeerClass, fromChannelID string) []db.Discovery {
	var discoveries []db.Discovery

	for _, peer := range peers {
		switch p := peer.(type) {
		case *tg.PeerChannel:
			discoveries = append(discoveries, db.Discovery{TGPeerID: p.ChannelID, SourceType: "requested_peer", FromChannelID: fromChannelID})
		case *tg.PeerUser:
			discoveries = append(discoveries, db.Discovery{TGPeerID: p.UserID, SourceType: "requested_peer", FromChannelID: fromChannelID})
		}
	}

	return discoveries
}
