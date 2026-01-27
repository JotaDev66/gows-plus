package server

import (
	"context"
	"encoding/json"
	"github.com/devlikeapro/gows/proto"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"time"
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
	if msg == nil && req.MessageId != "" {
		cli.Log.Debugf("Fetching message from storage '%s'", req.MessageId)
		storedMessage, err := cli.Storage.Messages.GetMessageWithRetries(req.GetMessageId())
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

	resp, err := cli.DownloadAnyMedia(ctx, msg)
	if err != nil {
		if ctx.Err() != nil {
			cli.Log.Warnf("Media download for '%s' canceled: %v", req.MessageId, ctx.Err())
			return nil, status.Error(codes.DeadlineExceeded, "download media timed out")
		}
		cli.Log.Errorf("Failed to download media for '%s' message: %v", req.MessageId, err)
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
