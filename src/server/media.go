package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/devlikeapro/gows/storage"
	__ "github.com/devlikeapro/gows/proto"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const downloadMediaTimeout = 10 * time.Minute

func (s *Server) DownloadMedia(ctx context.Context, req *__.DownloadMediaRequest) (*__.DownloadMediaResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadMediaTimeout)
	defer cancel()

	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	// Parse Message from JSON provided
	msg, buildMessageError := BuildMessage(req.GetMessage())
	if buildMessageError != nil {
		cli.Log.Warnf("Failed to build message from JSON: %v", buildMessageError)
	}

	// If parsing JSON failed - fetch it from storage
	var storedMessage *storage.StoredMessage
	if msg == nil && req.MessageId != "" {
		cli.Log.Debugf("Fetching message from storage '%s'", req.MessageId)
		storedMessage, err = cli.Storage.Messages.GetMessageWithRetries(req.GetMessageId())
		if err != nil {
			cli.Log.Warnf("Failed to fetch message '%s' from storage: %v", req.MessageId, err)
		}
		if storedMessage != nil {
			cli.Log.Infof("Found message '%s' in storage, using it to fetch media", req.MessageId)
			switch {
			case storedMessage.Message != nil && storedMessage.Message.Message != nil:
				msg = storedMessage.Message.Message
			case storedMessage.Message != nil && storedMessage.Message.RawMessage != nil:
				// History sync messages may only have RawMessage populated.
				msg = storedMessage.Message.RawMessage
			}
		}
	}

	if msg == nil {
		cli.Log.Warnf("Failed to build message '%s' from JSON or fetch storage", req.MessageId)
		if buildMessageError != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to parse message: %v", buildMessageError)
		}
		return nil, status.Error(codes.InvalidArgument, "message is empty")
	}

	if ctx.Err() != nil {
		return nil, status.Error(codes.DeadlineExceeded, "download media timed out before start")
	}

	// Build MessageInfo for the media-retry protocol (used on HTTP 403).
	// We always know chat JID and message ID from the request.
	// IsFromMe=false because inbound messages are the ones that hit 403.
	// If we fetched from storage we already have the full Info there.
	msgInfo := buildMessageInfo(req.GetJid(), req.GetMessageId(), storedMessage)

	resp, err := cli.DownloadAnyMediaWithRetry(ctx, msg, msgInfo)
	if err != nil {
		if ctx.Err() != nil {
			cli.Log.Warnf("Media download for '%s' canceled: %v", req.MessageId, ctx.Err())
			return nil, status.Error(codes.DeadlineExceeded, "download media timed out")
		}
		cli.Log.Errorf("Failed to download media for '%s' message: %v", req.MessageId, err)
		// Definitive media errors: the CDN rejected or lost the object and the
		// anonymous + media-retry fallbacks could not recover it. These are not
		// transient, so surface a non-retriable status to stop the caller from
		// re-issuing the call (each attempt can block on a media-retry wait).
		switch {
		case errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403):
			return nil, status.Errorf(codes.FailedPrecondition, "failed to download media: %v", err)
		case errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404),
			errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410):
			return nil, status.Errorf(codes.NotFound, "failed to download media: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to download media: %v", err)
	}
	if req.GetContentPath() != "" {
		if err := os.WriteFile(req.GetContentPath(), resp, 0644); err != nil {
			// Fallback to returning the content in the response
			cli.Log.Errorf("Failed to write media to '%s': %v", req.GetContentPath(), err)
			return &__.DownloadMediaResponse{Content: resp}, nil
		}
		return &__.DownloadMediaResponse{
			Content:     []byte{},
			ContentPath: req.GetContentPath(),
		}, nil
	}
	return &__.DownloadMediaResponse{Content: resp}, nil
}

// BuildMessage builds a message from the given JSON data
func BuildMessage(data string) (*waE2E.Message, error) {
	var message waE2E.Message
	err := json.Unmarshal([]byte(data), &message)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

// buildMessageInfo constructs a *types.MessageInfo for use with the media-retry protocol.
// It prefers the full info from storage (which includes IsGroup, IsFromMe, Sender).
// When storage is unavailable it builds a minimal info from the request fields.
func buildMessageInfo(jid, messageID string, stored *storage.StoredMessage) *types.MessageInfo {
	if stored != nil && stored.Message != nil {
		info := stored.Message.Info
		return &info
	}
	chatJID, err := types.ParseJID(jid)
	if err != nil {
		return &types.MessageInfo{
			ID: types.MessageID(messageID),
		}
	}
	return &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			IsFromMe: false,
			IsGroup:  chatJID.Server == types.GroupServer,
		},
		ID: types.MessageID(messageID),
	}
}
