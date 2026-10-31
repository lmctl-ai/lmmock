package mock

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	lmcode "github.com/lmctl-ai/lmcode-go"
)

// Compressed JSONL is a local sidecar persistence wire format, not provider
// policy. Paths/data come from trusted lmcode, like existing file_* helpers.
func registerSessionRecords(reg *lmcode.Registry) {
	if err := reg.Function("append_zstd", []string{"string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) {
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		if err != nil {
			return lmcode.None(), err
		}
		defer encoder.Close()
		data := encoder.EncodeAll([]byte(a[1].Str), nil)
		if err = os.MkdirAll(filepath.Dir(a[0].Str), 0700); err != nil {
			return lmcode.None(), err
		}
		file, err := os.OpenFile(a[0].Str, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return lmcode.None(), err
		}
		defer file.Close()
		n, err := file.Write(data)
		if err == nil && n != len(data) {
			err = errors.New("short compressed record write")
		}
		return lmcode.None(), err
	}); err != nil {
		panic(err)
	}
	if err := reg.Function("read_zstd", []string{"string"}, "string", func(a []lmcode.Value) (lmcode.Value, error) {
		data, err := os.ReadFile(a[0].Str)
		if os.IsNotExist(err) {
			return lmcode.String(""), nil
		}
		if err != nil {
			return lmcode.None(), err
		}
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return lmcode.None(), err
		}
		defer decoder.Close()
		out, err := decoder.DecodeAll(data, nil)
		return lmcode.String(string(out)), err
	}); err != nil {
		panic(err)
	}
}
