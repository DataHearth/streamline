{{- define "streamline.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "streamline.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "streamline.labels" -}}
app.kubernetes.io/name: {{ include "streamline.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "streamline.selectorLabels" -}}
app.kubernetes.io/name: {{ include "streamline.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "streamline.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{ default (include "streamline.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
{{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{- define "streamline.imageRef" -}}
{{ printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end -}}

{{- /* SCOPE — read before extending this. The guard checks the write paths set
       under .Values.config, and nothing else. It CANNOT check paths injected
       through .Values.extraEnv or .Values.secrets: koanf loads the STREAMLINE_*
       env provider AFTER the config file (internal/config/config.go, finalize),
       so env wins over everything rendered into the ConfigMap and the chart has
       no way to see the effective value. `extraEnv: [{name:
       STREAMLINE_LIBRARY__MOVIE_PATH, value: /var/movies}]` renders clean here
       and still dies on EROFS at startup. That hole is by construction, not an
       oversight — do not try to close it by pattern-matching env var names onto
       config keys, which only moves the wrongness around. Say so in values.yaml
       instead.

       What IS checked, and why each one matters:

       readOnlyRootFilesystem makes anything that is not a volume mount
       unwritable, and the app mkdirs at startup, in this order
       (internal/server/wire.go):
         1. <data_dir>/posters                      (internal/posters)
         2. <download_dir>/.streamline-session      (internal/bittorrent/engine.go,
            only when an ENABLED client_type=builtin download client exists —
            config.BuiltinDownloadClient gates on `enabled`)
         3. the movie and series roots
       Any of them outside a mount is EROFS before the first request, i.e. a
       crashloop whose only clue is one log line. Catch it at render time
       instead. Relative paths resolve against the image's WORKDIR, which
       deploy/Dockerfile pins to `/`.

       log.app.output / log.http.output are checked too, and their failure mode
       is worse: timberjack opens the file lazily on the first write and slog
       discards handler errors (internal/observability/writer.go), so an
       unwritable log path produces a healthy-looking pod that emits nothing at
       all. Only ABSOLUTE values need checking — a relative one joins onto
       data_dir, which is already in the set.

       /tmp is deliberately NOT an acceptable root even though it is a mount:
       it is a 64Mi emptyDir (see deployment.yaml), so anything placed there
       breaches sizeLimit and the kubelet evicts the pod — a slower, noisier
       failure than EROFS but no less fatal.

       NOT checked, deliberately: config.data_dir being a SUBDIRECTORY of a
       mount rather than the mount itself. That does fail, but not because of
       the root filesystem — see the config.data_dir note in values.yaml. */ -}}
{{- define "streamline.assertWritablePaths" -}}
{{- /* `securityContext: null` is a supported opt-out: deployment.yaml wraps the
       block in `{{ with }}`, so null means no container securityContext and a
       writable rootfs. Reaching through it unguarded turns that into a raw
       "nil pointer evaluating interface {}" template error. */ -}}
{{- if (.Values.securityContext | default dict).readOnlyRootFilesystem -}}
{{- $roots := list -}}
{{- if .Values.persistence.enabled -}}{{- $roots = append $roots "/data" -}}{{- end -}}
{{- range list .Values.library.shared .Values.library.media .Values.library.downloads -}}
{{- if .enabled -}}{{- $roots = append $roots .mountPath -}}{{- end -}}
{{- end -}}
{{- $lib := .Values.config.library | default dict -}}
{{- $shared := .Values.library.shared -}}
{{- $startup := dict "config.data_dir" (.Values.config.data_dir | default "./data") -}}
{{- if $shared.enabled -}}
{{- $_ := set $startup "config.library.movie_path" ($lib.movie_path | default (printf "%s/media/movies" $shared.mountPath)) -}}
{{- $_ = set $startup "config.library.series_path" ($lib.series_path | default (printf "%s/media/series" $shared.mountPath)) -}}
{{- else -}}
{{- $_ := set $startup "config.library.movie_path" ($lib.movie_path | default "/media/movies") -}}
{{- $_ = set $startup "config.library.series_path" ($lib.series_path | default "/media/series") -}}
{{- end -}}
{{- /* Same range the NetworkPolicy uses for the peer port, narrowed to enabled
       entries because a disabled one never reaches bittorrent.New. Validate
       rejects more than one builtin client, so at most one key is added. */ -}}
{{- range $i, $dc := .Values.config.download_clients | default list -}}
{{- if and (eq ($dc.client_type | default "") "builtin") $dc.enabled ($dc.download_dir | default "") -}}
{{- $_ := set $startup (printf "config.download_clients[%d].download_dir" $i) $dc.download_dir -}}
{{- end -}}
{{- end -}}
{{- $logCfg := .Values.config.log | default dict -}}
{{- $silent := dict -}}
{{- range $sink := list "app" "http" -}}
{{- $out := (get $logCfg $sink | default dict).output | default "" -}}
{{- if hasPrefix "/" $out -}}
{{- $_ := set $silent (printf "config.log.%s.output" $sink) $out -}}
{{- end -}}
{{- end -}}
{{- $mounted := ternary "none — persistence and every library volume are disabled" (join ", " $roots) (empty $roots) -}}
{{- range $mode, $set := dict "startup" $startup "logging" $silent -}}
{{- range $key, $path := $set -}}
{{- $abs := clean (printf "/%s" (trimPrefix "/" $path)) -}}
{{- $ok := false -}}
{{- range $root := $roots -}}
{{- if hasPrefix (printf "%s/" (clean $root)) (printf "%s/" $abs) -}}{{- $ok = true -}}{{- end -}}
{{- end -}}
{{- if not $ok -}}
{{- $envNote := printf "Redirecting %s through extraEnv or secrets does NOT satisfy this check and does NOT fix the pod: STREAMLINE_* env overrides the config file, so the chart still cannot see the effective path." $key -}}
{{- if hasPrefix "/tmp/" (printf "%s/" $abs) -}}
{{- $why := ternary "the data written there (SQLite database, poster cache, torrent payload) would breach the cap and get the pod evicted" "rotated log files would breach the cap and get the pod evicted, and every line written before the restart is gone regardless" (eq $mode "startup") -}}
{{- fail (printf "%s=%s resolves to %s, which is on the /tmp emptyDir. That volume is capped at sizeLimit 64Mi and is wiped on every restart, so %s. Point it at a persistent mount (%s), or set securityContext.readOnlyRootFilesystem=false to take ownership of this yourself. %s" $key $path $abs $why $mounted $envNote) -}}
{{- else -}}
{{- $why := ternary "the app mkdirs it before serving anything, so the pod would crashloop on \"read-only file system\"" "the log file cannot be created there — and that failure is SILENT, because timberjack opens the file lazily and slog discards handler errors, so you get a healthy-looking pod that logs nothing at all" (eq $mode "startup") -}}
{{- fail (printf "%s=%s resolves to %s, which is not under any persistent mounted volume (%s), and securityContext.readOnlyRootFilesystem is true — %s. Point it at a mounted volume, enable the volume that backs it, or set securityContext.readOnlyRootFilesystem=false. %s" $key $path $abs $mounted $why $envNote) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "streamline.otelEndpoint" -}}
{{- if .Values.observability.enabled -}}
alloy.{{ .Values.observability.namespace | default "observability" }}.svc.cluster.local:4318
{{- else -}}
{{ (.Values.config.otel | default dict).endpoint }}
{{- end -}}
{{- end -}}
