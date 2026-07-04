package server

import (
	"context"
	"fmt"

	"github.com/devlikeapro/gows/proto"
	"go.mau.fi/whatsmeow/types"
)

func (s *Server) RejectCall(ctx context.Context, req *__.RejectCallRequest) (*__.Empty, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	from, err := types.ParseJID(req.GetFrom())
	if err != nil {
		return nil, fmt.Errorf("parse from JID '%s': %w", req.GetFrom(), err)
	}
	if err = cli.RejectCall(ctx, from, req.GetId()); err != nil {
		return nil, err
	}
	return &__.Empty{}, nil
}

func (s *Server) StartCall(ctx context.Context, req *__.StartCallRequest) (*__.StartCallResponse, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	if cli.Calls == nil {
		return nil, fmt.Errorf("call controller not available for session")
	}
	to, err := types.ParseJID(req.GetTo())
	if err != nil {
		return nil, fmt.Errorf("parse to JID '%s': %w", req.GetTo(), err)
	}
	callID, err := cli.Calls.StartCall(ctx, to, req.GetAudioIn(), req.GetAudioOut())
	if err != nil {
		return nil, err
	}
	return &__.StartCallResponse{Id: callID}, nil
}

func (s *Server) AcceptCall(ctx context.Context, req *__.AcceptCallRequest) (*__.Empty, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	if cli.Calls == nil {
		return nil, fmt.Errorf("call controller not available for session")
	}
	if err = cli.Calls.AcceptCall(ctx, req.GetId(), req.GetAudioIn(), req.GetAudioOut()); err != nil {
		return nil, err
	}
	return &__.Empty{}, nil
}

func (s *Server) EndCall(ctx context.Context, req *__.EndCallRequest) (*__.Empty, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	if cli.Calls == nil {
		return nil, fmt.Errorf("call controller not available for session")
	}
	if err = cli.Calls.EndCall(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &__.Empty{}, nil
}

func (s *Server) WebRTC(ctx context.Context, req *__.WebRTCRequest) (*__.WebRTCResponse, error) {
	cli, err := s.Sm.Get(req.GetSession().GetId())
	if err != nil {
		return nil, err
	}
	if cli.Calls == nil {
		return nil, fmt.Errorf("call controller not available for session")
	}
	answer, err := cli.Calls.WebRTC(req.GetId(), req.GetSdpOffer())
	if err != nil {
		return nil, err
	}
	return &__.WebRTCResponse{SdpAnswer: answer}, nil
}
