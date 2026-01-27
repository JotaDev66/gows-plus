package server

import (
	"context"
	"fmt"

	__ "github.com/devlikeapro/gows/proto"
	"github.com/devlikeapro/gows/storage"
	"go.mau.fi/whatsmeow/types"
)

func (s *Server) GetMessageById(ctx context.Context, req *__.EntityByIdRequest) (*__.Json, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	id := req.Id
	msg, err := cli.Storage.Messages.GetMessageWithRetries(id)
	if err != nil {
		return nil, fmt.Errorf("error getting message by id %v: %w", id, err)
	}
	response, err := toJson(msg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling message %v: %w", id, err)
	}
	return response, nil
}

func (s *Server) GetMessages(ctx context.Context, req *__.GetMessagesRequest) (*__.JsonList, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	pagination := toPagination(req.Pagination)
	filters, err := parseMessageFilters(req.Filters)
	if err != nil {
		return nil, err
	}

	sort := toStorageSort(req.SortBy)
	if sort.Field == "" {
		sort.Field = "timestamp"
	}
	if sort.Order == "" {
		sort.Order = storage.SortDesc
	}
	messages, err := cli.Storage.Messages.GetAllMessages(*filters, sort, pagination)
	if err != nil {
		return nil, err
	}
	response, err := toJsonList(messages)
	if err != nil {
		return nil, fmt.Errorf("error marshaling messages: %w", err)
	}
	return response, nil
}

func parseMessageFilters(reqFilters *__.MessageFilters) (*storage.MessageFilter, error) {
	filters := storage.MessageFilter{}
	if reqFilters.Jid != nil {
		jid, err := types.ParseJID(reqFilters.Jid.Value)
		if err != nil {
			return nil, fmt.Errorf("error parsing jid %v: %w", reqFilters.Jid.Value, err)
		}
		filters.Jid = &jid
	}
	if reqFilters.TimestampGte != nil {
		filters.TimestampGte = parseTimeS(reqFilters.TimestampGte.Value)
	}
	if reqFilters.TimestampLte != nil {
		filters.TimestampLte = parseTimeS(reqFilters.TimestampLte.Value)
	}
	if reqFilters.FromMe != nil {
		filters.FromMe = &reqFilters.FromMe.Value
	}
	if reqFilters.Status != nil {
		status := storage.Status(reqFilters.Status.Value)
		filters.Status = &status
	}
	return &filters, nil
}

func toPagination(pagination *__.Pagination) storage.Pagination {
	return storage.Pagination{
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}
}

func toStorageSort(sortBy *__.SortBy) storage.Sort {
	var order storage.SortOrder
	switch sortBy.Order {
	case __.SortBy_ASC:
		order = storage.SortAsc
	case __.SortBy_DESC:
		order = storage.SortDesc
	}

	sort := storage.Sort{
		Field: sortBy.Field,
		Order: order,
	}
	return sort
}

func (s *Server) GetChats(ctx context.Context, req *__.GetChatsRequest) (*__.JsonList, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	pagination := toPagination(req.Pagination)
	sort := toStorageSort(req.SortBy)

	// Create an empty filter
	filter := storage.ChatFilter{}
	if req.GetFilter() != nil {
		jids := make([]types.JID, len(req.Filter.Jids))
		for _, jid := range req.GetFilter().Jids {
			jid, err := types.ParseJID(jid)
			if err != nil {
				return nil, fmt.Errorf("error parsing jid %v: %w", jid, err)
			}
			jids = append(jids, jid)
		}
		filter.Jids = jids
	}

	chats, err := cli.Storage.Chats.GetChats(filter, sort, pagination)
	if err != nil {
		return nil, err
	}
	response, err := toJsonList(chats)
	if err != nil {
		return nil, fmt.Errorf("error marshaling chats: %w", err)
	}
	return response, nil
}
