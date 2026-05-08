package daemon

import (
	"io"
	"net"
	"sync"

	"github.com/NyaMisty/dap-cli-go/internal/ipc"
)

type ClientHandler struct {
	app      *App
	conn     net.Conn
	decoder  *ipc.Decoder
	encoder  *ipc.Encoder
	sendMu   sync.Mutex
	clientID string
}

func NewClientHandler(app *App, conn net.Conn) *ClientHandler {
	return &ClientHandler{app: app, conn: conn, decoder: ipc.NewDecoder(conn), encoder: ipc.NewEncoder(conn)}
}

func (h *ClientHandler) Run() {
	defer func() {
		h.app.unregister(h)
		_ = h.conn.Close()
	}()
	for {
		message, err := h.decoder.Decode()
		if err != nil {
			if err != io.EOF {
				h.app.logger.Debug("ipc read failed", "error", err)
			}
			return
		}
		response := h.app.Handle(h, message)
		if err := h.Send(response); err != nil {
			return
		}
	}
}

func (h *ClientHandler) Send(message ipc.Envelope) error {
	h.sendMu.Lock()
	defer h.sendMu.Unlock()
	return h.encoder.Encode(message)
}
