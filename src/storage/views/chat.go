package views

import (
	"github.com/devlikeapro/gows/storage"
	"go.mau.fi/whatsmeow/types"
)

type ChatView struct {
	Messages storage.MessageStorage
	Contacts storage.ContactStorage
	Groups   storage.GroupStorage
}

var _ storage.ChatStorage = (*ChatView)(nil)

func NewChatView(message storage.MessageStorage, contacts storage.ContactStorage, groups storage.GroupStorage) *ChatView {
	return &ChatView{
		Messages: message,
		Contacts: contacts,
		Groups:   groups,
	}
}

func (s ChatView) GetChats(filter storage.ChatFilter, sortBy storage.Sort, pagination storage.Pagination, merge bool) ([]*storage.StoredChat, error) {
	if sortBy.Field == "id" {
		sortBy.Field = "jid"
	}
	messages, err := s.Messages.GetLastMessagesInChats(filter, sortBy, pagination, merge)
	if err != nil {
		return nil, err
	}

	// ignore Name for now, only show Jid and ConversationTimestamp
	chats := make([]*storage.StoredChat, len(messages))
	for i, msg := range messages {
		var name string
		if msg.Info.Chat.Server == types.GroupServer {
			group, _ := s.Groups.GetGroup(msg.Info.Chat)
			if group != nil {
				name = group.Name
			}
		} else if msg.Info.Chat.Server == types.DefaultUserServer {
			contact, err := s.Contacts.GetContact(msg.Info.Chat)
			switch {
			case err != nil:
				name = ""
			case contact == nil:
				name = ""
			case contact.Name != "":
				name = contact.Name
			case contact.PushName != "":
				name = contact.PushName
			}
		}
		chat := &storage.StoredChat{
			Jid:                   msg.Info.Chat,
			ConversationTimestamp: msg.Info.Timestamp,
			Name:                  name,
		}
		chats[i] = chat
	}
	return chats, nil
}
