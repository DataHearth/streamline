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

{{- define "streamline.otelEndpoint" -}}
{{- if .Values.observability.enabled -}}
alloy.{{ .Values.observability.namespace | default "observability" }}.svc.cluster.local:4318
{{- else -}}
{{ (.Values.config.otel | default dict).endpoint }}
{{- end -}}
{{- end -}}
