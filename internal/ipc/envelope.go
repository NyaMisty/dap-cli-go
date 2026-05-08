package ipc

const ProtocolVersion = 1

const (
	KindHello    = "hello"
	KindRequest  = "request"
	KindResponse = "response"
	KindEvent    = "event"
	KindError    = "error"
)

type Envelope struct {
	V         int            `json:"v" msgpack:"v"`
	Kind      string         `json:"kind" msgpack:"kind"`
	Type      string         `json:"type" msgpack:"type"`
	Payload   map[string]any `json:"payload" msgpack:"payload"`
	ID        string         `json:"id,omitempty" msgpack:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty" msgpack:"session_id,omitempty"`
}

func NewEnvelope(kind, typ, id, sessionID string, payload map[string]any) Envelope {
	if payload == nil {
		payload = map[string]any{}
	}
	return Envelope{V: ProtocolVersion, Kind: kind, Type: typ, ID: id, SessionID: sessionID, Payload: payload}
}

func ErrorEnvelope(req Envelope, reason string) Envelope {
	return NewEnvelope(KindError, req.Type, req.ID, req.SessionID, map[string]any{"error": reason})
}
