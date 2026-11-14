//go:build !onnx

package onnx

import (
	"context"
	"io"
)

func Available() bool { return false }

func Load(cfg Config) (*Models, error) { return nil, ErrUnavailable }

func Bench(ctx context.Context, cfg Config, iters int, w io.Writer) error { return ErrUnavailable }
