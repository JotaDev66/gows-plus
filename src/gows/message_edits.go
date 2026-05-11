package gows

import (
	"context"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

func (gows *GoWS) handleSecretMessageEdit(ctx context.Context, msg *events.Message) {
	sem := msg.Message.GetSecretEncryptedMessage()
	decryptedMsg, err := gows.DecryptSecretEncryptedMessage(ctx, msg)
	if err != nil {
		gows.Log.Warnf("Failed to decrypt secret message edit %s: %v", msg.Info.ID, err)
		gows.emitEvent(msg)
		return
	}
	msg.Message = &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Key:           sem.GetTargetMessageKey(),
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			EditedMessage: decryptedMsg,
		},
	}
	gows.emitEvent(msg)
}
