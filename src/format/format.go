package format

import (
	"encoding/json"
	"errors"

	"github.com/vmihailenco/msgpack/v5"
)

type Format string

const (
	FormatJSON    Format = "json"
	FormatMsgPack Format = "msgpack"
)

type Encoder interface {
	Encode(v interface{}) ([]byte, error)
	Decode(data []byte, v interface{}) error
}

type JSONEncoder struct{}

func (e *JSONEncoder) Encode(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (e *JSONEncoder) Decode(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

type MsgPackEncoder struct{}

func (e *MsgPackEncoder) Encode(v interface{}) ([]byte, error) {
	return msgpack.Marshal(v)
}

func (e *MsgPackEncoder) Decode(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

func GetEncoder(format string) (Encoder, error) {
	switch Format(format) {
	case FormatJSON:
		return &JSONEncoder{}, nil
	case FormatMsgPack:
		return &MsgPackEncoder{}, nil
	default:
		return nil, errors.New("unsupported format")
	}
}
