package detect

import (
	"context"

	"github.com/amareshhebbar/rungated/internal/core"
)

type JudgeVerdict struct {
	Verdict    string  `json:"verdict"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	Model      string  `json:"model"`
	Micros     int64   `json:"micros"`
}

type Judge interface {
	Judge(ctx context.Context, a *core.Action, d *core.Decision, purpose string) (*JudgeVerdict, error)
}
