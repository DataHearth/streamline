package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/dbbackup"
	"github.com/urfave/cli/v3"
)

// backupCreate is `streamline backup`: a manual snapshot of the database, safe
// with the server running or stopped. It never opens the database through
// db.Open, which would migrate it — a newer binary run for a backup before an
// upgrade must copy the database as it stands.
func backupCreate(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() > 0 {
		return fmt.Errorf("unknown command %q (see streamline backup --help)",
			cmd.Args().First())
	}
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	dir := dbbackup.Dir(cfg.DataDir)
	b, err := dbbackup.SnapshotFile(
		ctx,
		cfg.DatabasePath(),
		dir,
		dbbackup.KindManual,
	)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	removed, err := dbbackup.Prune(dir, dbbackup.KindManual,
		int(cfg.Backup.Retention.Manual))
	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"warning: could not prune old manual backups: %v\n",
			err,
		)
	}
	for _, name := range removed {
		fmt.Fprintf(os.Stderr, "pruned %s\n", name)
	}
	fmt.Fprintf(os.Stderr, "backed up schema version %d (%s)\n",
		b.Version, humanSize(b.Size))
	// The path alone on stdout, so a script can pick it up.
	fmt.Println(b.Path)
	return nil
}

// backupList is `streamline backup list`.
func backupList(_ context.Context, cmd *cli.Command) error {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	all, err := dbbackup.List(dbbackup.Dir(cfg.DataDir))
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "no backups in %s\n", dbbackup.Dir(cfg.DataDir))
		return nil
	}
	fmt.Print(formatBackupTable(all))
	return nil
}

func formatBackupTable(all []dbbackup.Backup) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tKIND\tSCHEMA\tCREATED\tSIZE")
	for _, b := range all {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
			b.Name, b.Kind, b.Version,
			b.CreatedAt.Local().Format(time.RFC3339), humanSize(b.Size))
	}
	_ = w.Flush()
	return sb.String()
}

// restoreCmd is `streamline restore <file>`. The argument is a path, or the
// bare name of a file in the backups directory as `backup list` prints it.
func restoreCmd(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: streamline restore <file>")
	}
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	dir := dbbackup.Dir(cfg.DataDir)
	src, err := resolveBackup(dir, cmd.Args().First())
	if err != nil {
		return err
	}
	// Without --config the data_dir is the default ./data, and a restore
	// there would succeed and be read by nothing. Restoring where no database
	// exists is real (a new machine) but deliberate.
	if _, err := os.Stat(cfg.DatabasePath()); errors.Is(err, fs.ErrNotExist) &&
		!cmd.Bool("force") {
		return fmt.Errorf(
			"no database at %s: check --config, or pass --force to restore into an empty data_dir",
			cfg.DatabasePath(),
		)
	}
	latest, err := db.LatestSchemaVersion()
	if err != nil {
		return err
	}
	res, err := dbbackup.Restore(ctx, src, cfg.DatabasePath(), dir, latest)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	if res.Replaced != nil {
		fmt.Fprintf(
			os.Stderr,
			"kept the replaced database as %s\n",
			res.Replaced.Path,
		)
	}
	fmt.Fprintf(os.Stderr, "restored %s (schema version %d) to %s\n",
		src, res.Restored.Version, cfg.DatabasePath())
	if res.Restored.Version < latest {
		fmt.Fprintf(
			os.Stderr,
			"the next start migrates it from version %d to %d, taking a pre-migration backup first\n",
			res.Restored.Version,
			latest,
		)
	}
	return nil
}

func resolveBackup(dir, arg string) (string, error) {
	if _, err := os.Stat(arg); err == nil {
		return arg, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	b, err := dbbackup.Find(dir, arg)
	if err != nil {
		return "", fmt.Errorf(
			"%s: no such file, and no backup of that name in %s",
			arg,
			dir,
		)
	}
	return b.Path, nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
