package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/config"
)

// HookSettings is the hook a manifest lists to check what the config file
// gives it, before it runs.
const HookSettings = "settings"

const (
	hookSettingsPath = "/hooks/settings"
	// maxReason bounds what a guest's refusal adds to the status.
	maxReason = 300
)

// given is what an entry's config hands its guest on every call.
type given struct {
	settings map[string]json.RawMessage
	secrets  map[string]string
}

func (g *given) equal(o *given) bool {
	if g == nil || o == nil {
		return g == o
	}
	same := func(a, b json.RawMessage) bool { return bytes.Equal(a, b) }
	return maps.EqualFunc(g.settings, o.settings, same) &&
		maps.Equal(g.secrets, o.secrets)
}

// loadGiven encodes the entry's settings and reads its secret files. The
// errors name the setting or the secret, never a secret's content.
func loadGiven(e config.ExtensionEntry) (*given, error) {
	g := &given{}
	for key, v := range e.Settings {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("setting %q is not a JSON value", key)
		}
		if g.settings == nil {
			g.settings = map[string]json.RawMessage{}
		}
		g.settings[key] = raw
	}
	for name, path := range e.SecretFiles {
		//nolint:gosec // the path comes from the operator's config file
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("secret %q: cannot read %s", name, path)
		}
		secret := strings.TrimRight(string(raw), "\r\n")
		if secret == "" {
			return nil, fmt.Errorf("secret %q: %s is empty", name, path)
		}
		if g.secrets == nil {
			g.secrets = map[string]string{}
		}
		g.secrets[name] = secret
	}
	return g, nil
}

// checkGiven asks a module that listed the settings hook whether it accepts
// g, and returns its refusal.
func (r *Running) checkGiven(
	ctx context.Context,
	g *given,
	hosts []string,
) error {
	if !slices.Contains(r.module.Manifest().Hooks, HookSettings) {
		return nil
	}
	res, err := r.callWith(ctx, Request{
		Method: http.MethodPost,
		Path:   hookSettingsPath,
		Hosts:  hosts,
	}, g)
	if err != nil {
		return fmt.Errorf("the settings check failed: %w", err)
	}
	if res.Status >= http.StatusOK && res.Status < http.StatusMultipleChoices {
		return nil
	}
	reason := strings.TrimSpace(string(res.Body))
	if len(reason) > maxReason {
		reason = reason[:maxReason]
	}
	if reason == "" {
		reason = fmt.Sprintf("status %d", res.Status)
	}
	return fmt.Errorf("the extension refused its settings: %s", reason)
}
