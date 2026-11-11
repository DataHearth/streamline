package extension

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	extism "github.com/extism/go-sdk"
)

var (
	errScanDenied      = errors.New("library_scan is not granted to this extension")
	errScanUnavailable = errors.New(
		"the library scan is not available on this server",
	)
)

func (r *Running) scanner(ctx context.Context) (Scanner, error) {
	if !slices.Contains(r.grants.Services, serviceLibraryScan) {
		slog.WarnContext(ctx, "Extension asked for a service it was not granted",
			"extension", r.name, "service", serviceLibraryScan)
		return nil, errScanDenied
	}
	if r.mgr.scanner == nil {
		return nil, errScanUnavailable
	}
	return r.mgr.scanner, nil
}

func scanStatusHost(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
	stack[0] = scanReply(ctx, p, func(s Scanner) ScanResult {
		st, err := s.ScanStatus(ctx)
		if err != nil {
			return ScanResult{Error: err.Error()}
		}
		return ScanResult{ScanStatus: st}
	})
}

func scanStartHost(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
	stack[0] = scanReply(ctx, p, func(s Scanner) ScanResult {
		if err := s.StartScan(ctx); err != nil {
			return ScanResult{Error: err.Error()}
		}
		return ScanResult{}
	})
}

// scanReply writes the answer into guest memory and returns its offset, 0
// when there is nothing to answer with.
func scanReply(
	ctx context.Context,
	p *extism.CurrentPlugin,
	run func(Scanner) ScanResult,
) uint64 {
	r, _ := ctx.Value(runningKey{}).(*Running)
	if r == nil {
		return 0
	}
	res := ScanResult{}
	if s, err := r.scanner(ctx); err != nil {
		res.Error = err.Error()
	} else {
		res = run(s)
	}
	out, err := json.Marshal(res)
	if err != nil {
		return 0
	}
	offset, err := p.WriteBytes(out)
	if err != nil {
		return 0
	}
	return offset
}
