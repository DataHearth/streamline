package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"
)

var (
	ErrBadManifest = errors.New("extension manifest is not usable")
	ErrGuestFailed = errors.New("extension failed")
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

type SQL interface {
	Query(ctx context.Context, sql string, args []any) (SQLResult, error)
	Exec(ctx context.Context, sql string, args []any) (SQLResult, error)
}

type Module struct {
	compiled *extism.CompiledPlugin
	manifest Manifest
}

type callState struct {
	name string
	db   SQL
}

type callKey struct{}

func Compile(ctx context.Context, wasm []byte) (*Module, error) {
	m := &Module{}
	cfg := extism.PluginConfig{
		EnableWasi:    true,
		RuntimeConfig: wazero.NewRuntimeConfig().WithCloseOnContextDone(true),
	}
	manifest := extism.Manifest{
		Wasm:   []extism.Wasm{extism.WasmData{Data: wasm}},
		Memory: &extism.ManifestMemory{MaxPages: MemoryPages},
	}
	compiled, err := extism.NewCompiledPlugin(ctx, manifest, cfg, hostFunctions())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	m.compiled = compiled

	raw, err := m.call(ctx, "manifest", nil, nil)
	if err != nil {
		_ = compiled.Close(ctx)
		return nil, fmt.Errorf("%w: reading the manifest: %w", ErrBadManifest, err)
	}
	if m.manifest, err = parseManifest(raw); err != nil {
		_ = compiled.Close(ctx)
		return nil, err
	}
	return m, nil
}

func parseManifest(raw []byte) (Manifest, error) {
	var mf Manifest
	if err := json.Unmarshal(raw, &mf); err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	if mf.StreamlineAPI != APIVersion {
		return Manifest{}, fmt.Errorf(
			"%w: it targets contract version %d, this server serves %d",
			ErrBadManifest,
			mf.StreamlineAPI,
			APIVersion,
		)
	}
	if !namePattern.MatchString(mf.Name) {
		return Manifest{}, fmt.Errorf(
			"%w: name %q must match %s",
			ErrBadManifest,
			mf.Name,
			namePattern,
		)
	}
	for _, c := range mf.AccountCards {
		if c.Kind == CardSettings {
			if err := c.checkFields(); err != nil {
				return Manifest{}, err
			}
		}
	}
	return mf, nil
}

func (m *Module) Manifest() Manifest { return m.manifest }

func (m *Module) Close(ctx context.Context) error { return m.compiled.Close(ctx) }

// ponytail: one instance per request, pool them if instantiation shows up in extension.request spans
func (m *Module) Handle(
	ctx context.Context,
	db SQL,
	req Request,
	mounts wazero.FSConfig,
) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	ctx = context.WithValue(
		ctx,
		callKey{},
		&callState{name: m.manifest.Name, db: db},
	)

	in, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	out, err := m.call(ctx, "handle", in, mounts)
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(out, &resp); err != nil {
		return Response{}, fmt.Errorf(
			"%w: unreadable response: %w",
			ErrGuestFailed,
			err,
		)
	}
	if resp.ServeFile == nil && resp.ServePoster == nil && resp.SendFile == nil &&
		len(resp.Body) > MaxResponseBody {
		return Response{}, fmt.Errorf(
			"%w: response body is %d bytes, the limit is %d",
			ErrGuestFailed,
			len(resp.Body),
			MaxResponseBody,
		)
	}
	return resp, nil
}

func (m *Module) call(
	ctx context.Context,
	fn string,
	in []byte,
	mounts wazero.FSConfig,
) ([]byte, error) {
	mc := wazero.NewModuleConfig().WithSysWalltime().WithSysNanotime()
	if mounts != nil {
		mc = mc.WithFSConfig(mounts)
	}
	p, err := m.compiled.Instance(
		ctx,
		extism.PluginInstanceConfig{ModuleConfig: mc},
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGuestFailed, err)
	}
	defer func() { _ = p.Close(context.WithoutCancel(ctx)) }()

	rc, out, err := p.CallWithContext(ctx, fn, in)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGuestFailed, err)
	}
	if rc != 0 {
		return nil, fmt.Errorf(
			"%w: %s exited with %d: %s",
			ErrGuestFailed,
			fn,
			rc,
			p.GetError(),
		)
	}
	return out, nil
}

func hostFunctions() []extism.HostFunction {
	i64 := []extism.ValueType{extism.ValueTypeI64}
	return []extism.HostFunction{
		extism.NewHostFunctionWithStack("sql_query", sqlHost(SQL.Query), i64, i64),
		extism.NewHostFunctionWithStack("sql_exec", sqlHost(SQL.Exec), i64, i64),
		extism.NewHostFunctionWithStack("log", logHost, i64, nil),
		extism.NewHostFunctionWithStack("ffmpeg_run", ffmpegHost, i64, i64),
		extism.NewHostFunctionWithStack("scan_status", scanStatusHost, i64, i64),
		extism.NewHostFunctionWithStack("scan_start", scanStartHost, i64, i64),
	}
}

func sqlHost(
	run func(SQL, context.Context, string, []any) (SQLResult, error),
) extism.HostFunctionStackCallback {
	return func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
		offset := stack[0]
		stack[0] = 0
		st, _ := ctx.Value(callKey{}).(*callState)
		if st == nil || st.db == nil {
			return
		}
		raw, err := p.ReadBytes(offset)
		if err != nil {
			return
		}
		var call SQLCall
		if err := json.Unmarshal(raw, &call); err != nil {
			return
		}
		res, err := run(st.db, ctx, call.SQL, integralArgs(call.Args))
		if err != nil {
			slog.ErrorContext(
				ctx,
				"Extension SQL call failed",
				"extension",
				st.name,
				"error",
				err,
			)
			return
		}
		out, err := json.Marshal(res)
		if err != nil {
			return
		}
		if stack[0], err = p.WriteBytes(out); err != nil {
			stack[0] = 0
		}
	}
}

// JSON has one number type, so a whole number the guest sent as an integer
// arrives as float64; SQLite would bind it as a REAL and an id comparison
// against an INTEGER column would still match, but a stored value would not
// round-trip as an integer.
func integralArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		if f, ok := a.(float64); ok && f == float64(int64(f)) {
			out[i] = int64(f)
			continue
		}
		out[i] = a
	}
	return out
}

func logHost(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
	raw, err := p.ReadBytes(stack[0])
	if err != nil {
		return
	}
	var line struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		KV      []any  `json:"kv"`
	}
	if err := json.Unmarshal(raw, &line); err != nil {
		return
	}
	name := ""
	if st, _ := ctx.Value(callKey{}).(*callState); st != nil {
		name = st.name
	}
	level := slog.LevelInfo
	switch line.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.Log(
		ctx,
		level,
		line.Message,
		append([]any{"extension", name}, line.KV...)...)
}
