package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

const (
	// readLimit caps the size of a single incoming WebSocket message.
	readLimit = 4 * 1024 // 4 KiB
	// readTimeout is the deadline for reading from the WebSocket.
	readTimeout = 60 * time.Second
	// writeTimeout is the deadline for writing to the WebSocket.
	writeTimeout = 10 * time.Second
)

// ServeWS upgrades an HTTP connection to WebSocket and pumps messages between
// the client and the hub. The userID must already be authenticated by the
// caller.
func ServeWS(hub *Hub, lg *zap.Logger, w http.ResponseWriter, r *http.Request, userID int64) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The session cookie is already verified by the auth middleware; the
		// request host is always authorised by coder/websocket.
		InsecureSkipVerify: true,
	})
	if err != nil {
		lg.Error("ws accept", zap.Int64("user_id", userID), zap.Error(err))
		return
	}
	conn.SetReadLimit(readLimit)

	client := hub.NewClient(userID)
	hub.Register(client)
	defer func() {
		hub.Unregister(client)
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Write pump: delivers outgoing messages from the hub to the client.
	connCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go writePump(conn, client, connCtx, lg)

	// Read pump: reads incoming messages and dispatches them to the hub.
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		ctx, msgCancel := context.WithTimeout(r.Context(), 5*time.Second)
		switch env.Type {
		case "message":
			if err := hub.HandleMessage(ctx, userID, env); err != nil {
				lg.Error("handle message", zap.Int64("user_id", userID), zap.Error(err))
			}
		case "read":
			if err := hub.HandleRead(ctx, userID, env); err != nil {
				lg.Error("handle read", zap.Int64("user_id", userID), zap.Error(err))
			}
		}
		msgCancel()
	}
}

// writePump sends queued messages from the hub to the WebSocket client. Exits
// when the client's send channel is closed or the context is cancelled.
func writePump(conn *websocket.Conn, client *Client, ctx context.Context, lg *zap.Logger) {
	for {
		select {
		case payload, ok := <-client.Send():
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			if err := conn.Write(wctx, websocket.MessageText, payload); err != nil {
				cancel()
				lg.Debug("ws write", zap.Int64("user_id", client.UserID()), zap.Error(err))
				return
			}
			cancel()
		case <-ctx.Done():
			return
		}
	}
}
