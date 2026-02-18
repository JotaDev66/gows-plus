package gows

import (
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"time"
)

var (
	FetchPreviewTimeout = 6 * time.Second
)

func (gows *GoWS) BuildConversationMessage(text string) *waE2E.Message {
	message := waE2E.Message{}
	message.Conversation = proto.String(text)
	return &message
}

// BuildExtendedTextMessage builds a text message and adds a link preview if requested.
func (gows *GoWS) BuildExtendedTextMessage(text string) *waE2E.Message {
	message := waE2E.Message{}
	message.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
		Text: proto.String(text),
	}
	return &message
}

// BuildEditedMessage builds a new message for editing an existing message,
// preserving the original message's type and context info when possible.
func (gows *GoWS) BuildEditedMessage(
	jid types.JID,
	text string,
	originalMessage *waE2E.Message,
) *waE2E.Message {
	switch {
	case originalMessage != nil && originalMessage.GetConversation() != "":
		// Keep the current behavior for plain conversation messages.
		return gows.BuildConversationMessage(text)
	case originalMessage != nil && originalMessage.GetImageMessage() != nil:
		return &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption:     proto.String(text),
				ContextInfo: originalMessage.GetImageMessage().GetContextInfo(),
			},
		}
	case originalMessage != nil && originalMessage.GetVideoMessage() != nil:
		return &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption:     proto.String(text),
				ContextInfo: originalMessage.GetVideoMessage().GetContextInfo(),
			},
		}
	case originalMessage != nil && originalMessage.GetDocumentMessage() != nil:
		return &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				Caption:     proto.String(text),
				ContextInfo: originalMessage.GetDocumentMessage().GetContextInfo(),
			},
		}
	case originalMessage != nil &&
		originalMessage.GetDocumentWithCaptionMessage() != nil &&
		originalMessage.GetDocumentWithCaptionMessage().GetMessage() != nil &&
		originalMessage.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage() != nil:
		return &waE2E.Message{
			DocumentWithCaptionMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					DocumentMessage: &waE2E.DocumentMessage{
						Caption: proto.String(text),
						ContextInfo: originalMessage.GetDocumentWithCaptionMessage().
							GetMessage().
							GetDocumentMessage().
							GetContextInfo(),
					},
				},
			},
		}
	default:
		var contextInfo = ExtractContextInfo(&events.Message{Message: originalMessage})
		message := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(text),
				ContextInfo: contextInfo,
			},
		}
		return message
	}
}

// BuildEdit builds a message edit message using the given variables.
// The built message can be sent normally using Client.SendMessage.
//
// Adjusted from the original meow BuildEdit - it counts for participants (groups)
//
//	resp, err := cli.SendMessage(context.Background(), chat, cli.BuildEdit(chat, originalMessageID, &waE2E.Message{
//		Conversation: proto.String("edited message"),
//	})
func (gows *GoWS) BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message {
	key := &waCommon.MessageKey{
		FromMe:    proto.Bool(true),
		ID:        proto.String(id),
		RemoteJID: proto.String(chat.String()),
	}
	// If the chat is a group, set the participant
	if chat.Server == types.GroupServer {
		key.Participant = proto.String(gows.int.GetOwnID().ToNonAD().String())
	}
	protocol := &waE2E.ProtocolMessage{
		Key:           key,
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		EditedMessage: newContent,
		TimestampMS:   proto.Int64(time.Now().UnixMilli()),
	}

	// Keep newsletter edit encoding compatible with whatsmeow's newsletter sender path.
	if chat.Server == types.NewsletterServer {
		return &waE2E.Message{
			EditedMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					ProtocolMessage: protocol,
				},
			},
		}
	}

	return &waE2E.Message{
		ProtocolMessage: protocol,
	}
}

func (gows *GoWS) PopulateContextInfoWithReply(info *waE2E.ContextInfo, replyToId types.MessageID) (*waE2E.ContextInfo, error) {
	msg, err := gows.Storage.Messages.GetMessageWithRetries(replyToId)
	if err != nil {
		return info, err
	}

	if info == nil {
		info = &waE2E.ContextInfo{}
	}

	quoted := msg.Message.Message
	quoted.MessageContextInfo = nil
	info.StanzaID = proto.String(msg.Info.ID)
	info.Participant = proto.String(msg.Info.Sender.ToNonAD().String())
	info.QuotedMessage = quoted
	return info, nil
}

func (gows *GoWS) PopulateContextInfoWithMentions(info *waE2E.ContextInfo, mentions []string) *waE2E.ContextInfo {
	if len(mentions) == 0 {
		return info
	}
	if info == nil {
		info = &waE2E.ContextInfo{}
	}
	info.MentionedJID = mentions
	return info
}

func ExtractContextInfo(event *events.Message) *waE2E.ContextInfo {
	if event.Message == nil {
		return nil
	}
	msg := event.Message
	switch {
	case msg.Conversation != nil:
		return nil
	case msg.ExtendedTextMessage != nil:
		return msg.ExtendedTextMessage.ContextInfo
	case msg.ImageMessage != nil:
		return msg.ImageMessage.ContextInfo
	case msg.ContactMessage != nil:
		return msg.ContactMessage.ContextInfo
	case msg.LocationMessage != nil:
		return msg.LocationMessage.ContextInfo
	case msg.VideoMessage != nil:
		return msg.VideoMessage.ContextInfo
	case msg.PtvMessage != nil:
		return msg.PtvMessage.ContextInfo
	case msg.AudioMessage != nil:
		return msg.AudioMessage.ContextInfo
	case msg.DocumentMessage != nil:
		return msg.DocumentMessage.ContextInfo
	case msg.DocumentWithCaptionMessage != nil && msg.DocumentWithCaptionMessage.Message != nil && msg.DocumentWithCaptionMessage.Message.DocumentMessage != nil:
		return msg.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo
	case msg.StickerMessage != nil:
		return msg.StickerMessage.ContextInfo
	case msg.ContactsArrayMessage != nil:
		return msg.ContactsArrayMessage.ContextInfo
	case msg.TemplateMessage != nil:
		return msg.TemplateMessage.ContextInfo
	case msg.ListMessage != nil:
		return msg.ListMessage.ContextInfo
	case msg.PollCreationMessage != nil:
		return msg.PollCreationMessage.ContextInfo
	case msg.PollCreationMessageV2 != nil:
		return msg.PollCreationMessageV2.ContextInfo
	case msg.PollCreationMessageV3 != nil:
		return msg.PollCreationMessageV3.ContextInfo
	default:
		return nil
	}
}

type Contact struct {
	DisplayName string
	Vcard       string
}

func buildContactMessage(contact Contact) *waE2E.ContactMessage {
	return &waE2E.ContactMessage{
		DisplayName: proto.String(contact.DisplayName),
		Vcard:       proto.String(contact.Vcard),
	}
}

func BuildContactsMessage(contacts []Contact, contextInfo *waE2E.ContextInfo) (message *waE2E.Message) {
	if len(contacts) == 0 {
		return nil
	}

	// Single contact
	if len(contacts) == 1 {
		message = &waE2E.Message{
			ContactMessage: buildContactMessage(contacts[0]),
		}
		message.ContactMessage.ContextInfo = contextInfo
		return message
	}
	// Multiple contacts
	message = &waE2E.Message{
		ContactsArrayMessage: &waE2E.ContactsArrayMessage{
			Contacts: make([]*waE2E.ContactMessage, len(contacts)),
		},
	}
	for i, contact := range contacts {
		message.ContactsArrayMessage.Contacts[i] = buildContactMessage(contact)
	}
	message.ContactsArrayMessage.ContextInfo = contextInfo
	return message
}

func BuildContactUpdate(jid types.JID, firstName, lastName string) appstate.PatchInfo {
	fullName := firstName
	if lastName != "" {
		fullName = firstName + " " + lastName
	}

	return appstate.PatchInfo{
		Type: appstate.WAPatchCriticalUnblockLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexContact, jid.String()},
			Version: 2,
			Value: &waSyncAction.SyncActionValue{
				ContactAction: &waSyncAction.ContactAction{
					FullName:                 proto.String(fullName),
					FirstName:                proto.String(firstName),
					SaveOnPrimaryAddressbook: proto.Bool(true),
				},
			},
		}},
	}
}

// BuildChatUnread builds an app state patch for marking a chat as read or unread.
//
// The lastMessageKeys parameter should contain the message keys to mark as read.
// If markRead is true, the chat will be marked as read, otherwise it will be marked as unread.
// Note: Only MessageKey is accepted; timestamps are not required.
func BuildChatUnread(
	jid types.JID,
	markRead bool,
	lastMessageKeys []*waCommon.MessageKey,
	lastMessageTimestamp time.Time,
) appstate.PatchInfo {
	messageRange := &waSyncAction.SyncActionMessageRange{
		LastMessageTimestamp: proto.Int64(lastMessageTimestamp.UnixMilli()),
	}

	if len(lastMessageKeys) > 0 {
		// Convert keys to SyncActionMessage without timestamps, as only keys are required
		msgs := make([]*waSyncAction.SyncActionMessage, len(lastMessageKeys))
		for i, key := range lastMessageKeys {
			msgs[i] = &waSyncAction.SyncActionMessage{Key: key}
		}
		messageRange.Messages = msgs
	}

	return appstate.PatchInfo{
		Type: appstate.WAPatchRegularLow,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexMarkChatAsRead, jid.String()},
			Version: 3,
			Value: &waSyncAction.SyncActionValue{
				MarkChatAsReadAction: &waSyncAction.MarkChatAsReadAction{
					Read:         proto.Bool(markRead),
					MessageRange: messageRange,
				},
			},
		}},
	}
}
