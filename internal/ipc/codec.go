package ipc

import (
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

type Encoder struct {
	enc *msgpack.Encoder
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{enc: msgpack.NewEncoder(w)}
}

func (e *Encoder) Encode(message Envelope) error {
	return e.enc.Encode(message)
}

type Decoder struct {
	dec *msgpack.Decoder
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{dec: msgpack.NewDecoder(r)}
}

func (d *Decoder) Decode() (Envelope, error) {
	var message Envelope
	err := d.dec.Decode(&message)
	return message, err
}
