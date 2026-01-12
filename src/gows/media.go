package gows

import (
	"context"
	"fmt"
	"github.com/devlikeapro/gows/media"
	"github.com/gogo/protobuf/proto"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"time"
)

func (gows *GoWS) DownloadAnyMedia(ctx context.Context, msg *waE2E.Message) (data []byte, err error) {
	target := unwrapMediaMessage(msg)
	if target == nil {
		return nil, whatsmeow.ErrNothingDownloadableFound
	}
	switch {
	case target.ImageMessage != nil:
		return gows.Download(ctx, target.ImageMessage)
	case target.VideoMessage != nil:
		return gows.Download(ctx, target.VideoMessage)
	case target.PtvMessage != nil:
		return gows.Download(ctx, target.PtvMessage)
	case target.AudioMessage != nil:
		return gows.Download(ctx, target.AudioMessage)
	case target.DocumentMessage != nil:
		return gows.Download(ctx, target.DocumentMessage)
	case target.DocumentWithCaptionMessage != nil:
		return gows.Download(ctx, target.DocumentWithCaptionMessage.Message.DocumentMessage)
	case target.StickerMessage != nil:
		return gows.Download(ctx, target.StickerMessage)
	default:
		return nil, whatsmeow.ErrNothingDownloadableFound
	}
}

func unwrapMediaMessage(msg *waE2E.Message) *waE2E.Message {
	if msg == nil {
		return nil
	}
	if hasMediaPayload(msg) {
		return msg
	}
	nested := []*waE2E.Message{
		getFutureProofMessage(msg.GetEphemeralMessage()),
		getFutureProofMessage(msg.GetViewOnceMessage()),
		getFutureProofMessage(msg.GetViewOnceMessageV2()),
		getFutureProofMessage(msg.GetViewOnceMessageV2Extension()),
		getFutureProofMessage(msg.GetAssociatedChildMessage()),
	}
	for _, child := range nested {
		if child == nil {
			continue
		}
		if resolved := unwrapMediaMessage(child); resolved != nil {
			return resolved
		}
	}
	return nil
}

func getFutureProofMessage(container *waE2E.FutureProofMessage) *waE2E.Message {
	if container == nil {
		return nil
	}
	return container.Message
}

func hasMediaPayload(msg *waE2E.Message) bool {
	if msg == nil {
		return false
	}
	return msg.ImageMessage != nil ||
		msg.VideoMessage != nil ||
		msg.PtvMessage != nil ||
		msg.AudioMessage != nil ||
		msg.DocumentMessage != nil ||
		msg.DocumentWithCaptionMessage != nil ||
		msg.StickerMessage != nil
}

func (gows *GoWS) UploadMedia(
	ctx context.Context,
	jid types.JID,
	content []byte,
	mediaType whatsmeow.MediaType,
) (resp whatsmeow.UploadResponse, err error) {
	if IsNewsletter(jid) {
		resp, err = gows.UploadNewsletter(ctx, content, mediaType)
	} else {
		resp, err = gows.Upload(ctx, content, mediaType)
	}
	return resp, err
}

// AddLinkPreviewSafe adds a link preview to the message if a link is found in the text.
// logs an error if the preview cannot be fetched.
func (gows *GoWS) AddLinkPreviewSafe(jid types.JID, message *waE2E.ExtendedTextMessage, highQuality bool, preview *media.LinkPreview) {
	linkPreviewCtx, cancel := context.WithTimeout(gows.Context, FetchPreviewTimeout)
	defer cancel()
	err := gows.AddLinkPreviewWithContext(linkPreviewCtx, jid, message, highQuality, preview)
	if err != nil {
		gows.Log.Warnf("Failed to add link preview: %v", err)
	}
}

// AddLinkPreviewWithContext adds a link preview to the message if a link is found in the text.
// returns an error if the preview cannot be fetched.
func (gows *GoWS) AddLinkPreviewWithContext(
	ctx context.Context,
	jid types.JID,
	message *waE2E.ExtendedTextMessage,
	highQuality bool,
	preview *media.LinkPreview,
) (err error) {
	var matched string

	if preview == nil {
		// If the preview is nil, we need to extract the URL from the text
		text := message.Text
		matched = media.ExtractUrlFromText(*text)
		if matched == "" {
			return nil
		}
		// "matched" must be exact as it was in the text
		// but scraped URL should be normalized (because it'd also find www.whatsapp.com)
		url := media.MakeSureURL(matched)
		preview, err = media.GoscraperFetchPreview(ctx, url)
		if err != nil || preview == nil {
			return fmt.Errorf("failed to fetch preview info for (%s): %w", url, err)
		}
	} else {
		// If the preview provided, we need to extract the URL from it
		matched = preview.Url
	}

	type_ := waE2E.ExtendedTextMessage_NONE
	message.PreviewType = &type_
	message.MatchedText = &matched
	message.Title = &preview.Title
	message.Description = &preview.Description

	var image []byte
	switch {
	case preview.Image != nil && len(preview.Image) > 0:
		gows.Log.Debugf("Using image data provided from link preview")
		image = preview.Image
	case preview.ImageUrl != "":
		gows.Log.Debugf("Using image URL (%s) from link preview", preview.ImageUrl)
		image, err = media.FetchBodyByUrl(ctx, preview.ImageUrl)
		if err != nil {
			return fmt.Errorf("failed to download image (%s) for link preview: %w", preview.ImageUrl, err)
		}
	case preview.IconUrl != "":
		gows.Log.Debugf("Using icon URL (%s) from link preview", preview.IconUrl)
		image, err = media.FetchBodyByUrl(ctx, preview.IconUrl)
		if err != nil {
			return fmt.Errorf("failed to download icon (%s) for link preview: %w", preview.IconUrl, err)
		}
		highQuality = false
	default:
		gows.Log.Debugf("No image or icon URL found in link preview")
		return nil
	}

	if !highQuality {
		thumbnail, err := media.Resize(image, media.PreviewLinkBuiltInSize)
		if err != nil {
			return fmt.Errorf("failed to generate thumbnail: %w", err)
		}
		message.JPEGThumbnail = thumbnail
	} else {
		thumbnail, err := media.ImageAutoThumbnail(image)
		if err != nil {
			return fmt.Errorf("failed to generate thumbnail: %w", err)
		}
		resp, err := gows.UploadMedia(gows.Context, jid, image, whatsmeow.MediaLinkThumbnail)
		if err != nil {
			return fmt.Errorf("failed to upload image (%s): %w", preview.ImageUrl, err)
		}
		size, err := media.CurrentSize(image)
		if err != nil {
			size = media.PreviewLinkSize
		}
		message.JPEGThumbnail = thumbnail
		message.ThumbnailDirectPath = &resp.DirectPath
		message.ThumbnailSHA256 = resp.FileSHA256
		message.ThumbnailEncSHA256 = resp.FileEncSHA256
		message.ThumbnailHeight = proto.Uint32(size.Height)
		message.ThumbnailWidth = proto.Uint32(size.Width)
		message.MediaKey = resp.MediaKey
		now := time.Now().Unix()
		message.MediaKeyTimestamp = &now
	}
	return nil
}
