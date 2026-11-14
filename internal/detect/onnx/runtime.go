//go:build onnx

package onnx

import (
	"os"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	initOnce sync.Once
	initErr  error
)

func Available() bool { return true }

func defaultLibrary() string {
	switch runtime.GOOS {
	case "darwin":
		return "libonnxruntime.dylib"
	case "windows":
		return "onnxruntime.dll"
	}
	return "libonnxruntime.so"
}

func initRuntime(lib string) error {
	initOnce.Do(func() {
		path := lib
		if path == "" {
			path = os.Getenv("ORT_LIB")
		}
		if path == "" {
			path = defaultLibrary()
		}
		ort.SetSharedLibraryPath(path)
		initErr = ort.InitializeEnvironment()
	})
	return initErr
}
