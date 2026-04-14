{{- define "observability.labels" -}}
app.kubernetes.io/part-of: streamline-observability
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
