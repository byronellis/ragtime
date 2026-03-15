package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// WriteMessage encodes an Envelope and writes it as a length-prefixed JSON message.
func WriteMessage(w io.Writer, env *Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// ReadMessage reads a length-prefixed JSON message and decodes it into an Envelope.
func ReadMessage(r io.Reader) (*Envelope, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}
	if length > 10*1024*1024 { // 10 MB sanity limit
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return &env, nil
}

// NewEnvelope creates an Envelope with the given type and payload.
func NewEnvelope(msgType MessageType, payload any) (*Envelope, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return &Envelope{
		Type:    msgType,
		Payload: json.RawMessage(data),
	}, nil
}

// DecodePayload unmarshals the envelope's payload into the given target.
func (e *Envelope) DecodePayload(target any) error {
	return json.Unmarshal(e.Payload, target)
}
