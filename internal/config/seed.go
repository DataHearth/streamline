package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/yaml"
)

// seedEnvKeys are the keys a seeded config file takes from the environment
// rather than from defaults().
//
// A container template (Unraid, Synology, TrueNAS) sets the library folders as
// STREAMLINE_* variables. Seeded with the defaults alone, the file would name
// /media/movies under a variable naming the share: every boot then warns that
// the environment shadows the file (warnEnvShadowsFile), and dropping the
// variable later moves the instance onto the default folders in silence. With
// the variable's value written in, the file is the durable record and the two
// agree.
//
// An allowlist, deliberately: a secret injected by environment is how a
// container keeps it off the volume, so nothing that is not a plain path may
// land on disk this way.
var seedEnvKeys = []string{
	"data_dir",
	"library.movie_path",
	"library.series_path",
	"library.music_path",
	"library.ebook_path",
	"library.audiobook_path",
	"library.download_path",
}

// Seed writes a default config file at path when nothing is there, so a
// container whose config directory is a fresh, empty volume boots on its first
// start instead of exiting until someone runs `config init` by hand. It reports
// whether it wrote one.
//
// The file is what `config init` writes, except for the keys in seedEnvKeys
// the environment sets. Anything already at path — an empty file, a dangling
// symlink, a file this process may not read — is left for Load to report. A
// directory Seed cannot write to is an error naming the fix, where Load would
// have said only "no such file or directory".
func Seed(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	k := newDefaultsKoanf()
	envK, err := loadEnv()
	if err != nil {
		return false, err
	}
	for _, key := range seedEnvKeys {
		if envK.Exists(key) {
			_ = k.Set(key, envK.Get(key))
		}
	}
	out, err := k.Marshal(yaml.Parser())
	if err != nil {
		return false, err
	}

	cannot := func(err error) error {
		return fmt.Errorf(
			"config file %s does not exist and cannot be created (this process runs as uid %d, gid %d): %w — "+
				"make its directory writable by that user, or write one with `streamline config init --output %s`",
			path,
			os.Getuid(),
			os.Getgid(),
			err,
			path,
		)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, cannot(err)
	}
	// Owner-only, as `config init` writes it: the file goes on to hold the
	// session secret and every integration's credentials. O_EXCL so a second
	// process starting on the same volume never truncates the first's file.
	//nolint:gosec // path is the operator's own --config flag
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, cannot(err)
	}
	_, err = f.Write(out)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		// A half-written file would fail every later boot; gone, the next one
		// seeds again.
		_ = os.Remove(path)
		return false, fmt.Errorf("write default config to %s: %w", path, err)
	}
	return true, nil
}
