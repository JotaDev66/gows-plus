package noop

import "github.com/devlikeapro/gows/storage"

type ChatStorage struct{}

var _ storage.ChatStorage = (*ChatStorage)(nil)

func NewChatStorage() *ChatStorage {
	return &ChatStorage{}
}

func (s ChatStorage) GetChats(filter storage.ChatFilter, sortBy storage.Sort, pagination storage.Pagination, merge bool) ([]*storage.StoredChat, error) {
	return []*storage.StoredChat{}, nil
}
