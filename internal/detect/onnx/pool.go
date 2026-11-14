//go:build onnx

package onnx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

type source struct {
	path string
	data []byte
}

func (s source) read() ([]byte, error) {
	if s.data != nil {
		return s.data, nil
	}
	return os.ReadFile(s.path)
}

type feed struct {
	shape []int64
	i64   []int64
	b     []bool
	f32   []float32
}

type output struct {
	data  []float32
	shape []int64
}

type pool struct {
	ch       chan *ort.DynamicAdvancedSession
	all      []*ort.DynamicAdvancedSession
	inputs   []ort.InputOutputInfo
	inNames  []string
	outNames []string
	once     sync.Once
}

func pickOutput(outs []ort.InputOutputInfo) (string, error) {
	if len(outs) == 0 {
		return "", errors.New("onnx: model has no outputs")
	}
	for _, want := range []string{"logits", "sentence_embedding"} {
		for _, o := range outs {
			if o.Name == want {
				return o.Name, nil
			}
		}
	}
	return outs[0].Name, nil
}

func newPool(src source, size, threads int) (*pool, error) {
	data, err := src.read()
	if err != nil {
		return nil, err
	}
	in, out, err := ort.GetInputOutputInfoWithONNXData(data)
	if err != nil {
		return nil, err
	}
	outName, err := pickOutput(out)
	if err != nil {
		return nil, err
	}
	p := &pool{ch: make(chan *ort.DynamicAdvancedSession, size), inputs: in, outNames: []string{outName}}
	for _, i := range in {
		p.inNames = append(p.inNames, i.Name)
	}
	for i := 0; i < size; i++ {
		opts, err := ort.NewSessionOptions()
		if err != nil {
			p.close()
			return nil, err
		}
		opts.SetIntraOpNumThreads(threads)
		opts.SetInterOpNumThreads(1)
		s, err := ort.NewDynamicAdvancedSessionWithONNXData(data, p.inNames, p.outNames, opts)
		opts.Destroy()
		if err != nil {
			p.close()
			return nil, err
		}
		p.all = append(p.all, s)
		p.ch <- s
	}
	return p, nil
}

func (p *pool) has(name string) bool {
	for _, n := range p.inNames {
		if n == name {
			return true
		}
	}
	return false
}

func (p *pool) close() {
	p.once.Do(func() {
		for _, s := range p.all {
			s.Destroy()
		}
	})
}

func (p *pool) run(ctx context.Context, feeds map[string]feed) (*output, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var s *ort.DynamicAdvancedSession
	select {
	case s = <-p.ch:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { p.ch <- s }()
	inputs := make([]ort.Value, 0, len(p.inputs))
	defer func() {
		for _, v := range inputs {
			v.Destroy()
		}
	}()
	for _, info := range p.inputs {
		f, ok := feeds[info.Name]
		if !ok {
			return nil, fmt.Errorf("onnx: no value for model input %q", info.Name)
		}
		v, err := makeTensor(info, f)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, v)
	}
	ro, err := ort.NewRunOptions()
	if err != nil {
		return nil, err
	}
	defer ro.Destroy()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			ro.Terminate()
		case <-done:
		}
	}()
	outs := []ort.Value{nil}
	err = s.RunWithOptions(inputs, outs, ro)
	close(done)
	wg.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	defer outs[0].Destroy()
	t, ok := outs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("onnx: output %q is not float32", p.outNames[0])
	}
	return &output{data: append([]float32(nil), t.GetData()...), shape: append([]int64(nil), t.GetShape()...)}, nil
}

func makeTensor(info ort.InputOutputInfo, f feed) (ort.Value, error) {
	shape := ort.NewShape(f.shape...)
	switch info.DataType {
	case ort.TensorElementDataTypeInt64:
		return ort.NewTensor(shape, f.i64)
	case ort.TensorElementDataTypeInt32:
		d := make([]int32, len(f.i64))
		for i, v := range f.i64 {
			d[i] = int32(v)
		}
		return ort.NewTensor(shape, d)
	case ort.TensorElementDataTypeBool:
		d := f.b
		if d == nil {
			d = make([]bool, len(f.i64))
			for i, v := range f.i64 {
				d[i] = v != 0
			}
		}
		return ort.NewTensor(shape, d)
	case ort.TensorElementDataTypeFloat:
		d := f.f32
		if d == nil {
			d = make([]float32, len(f.i64))
			for i, v := range f.i64 {
				d[i] = float32(v)
			}
		}
		return ort.NewTensor(shape, d)
	}
	return nil, fmt.Errorf("onnx: unsupported input type %v for %q", info.DataType, info.Name)
}
