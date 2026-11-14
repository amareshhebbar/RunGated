//go:build onnx && !onnx_embed

package onnx

func embeddedFile(name string) ([]byte, bool) { return nil, false }
