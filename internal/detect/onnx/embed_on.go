//go:build onnx && onnx_embed

package onnx

import "embed"

//go:embed all:models
var embeddedFS embed.FS

func embeddedFile(name string) ([]byte, bool) {
	b, err := embeddedFS.ReadFile("models/" + name)
	return b, err == nil
}
