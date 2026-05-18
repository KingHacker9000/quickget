package nativehost

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxMessageSize = 10 * 1024 * 1024

func ReadMessage(r io.Reader, out any) error {
	buf, err := ReadMessageBytes(r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(buf, out); err != nil {
		return fmt.Errorf("decode native message: %w", err)
	}
	return nil
}

func ReadMessageBytes(r io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, err
	}
	if size == 0 || size > maxMessageSize {
		return nil, fmt.Errorf("invalid native message size: %d", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func WriteMessage(w io.Writer, msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode native message: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxMessageSize {
		return fmt.Errorf("invalid payload size: %d", len(payload))
	}
	size := uint32(len(payload))
	if err := binary.Write(w, binary.LittleEndian, size); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}
