{{- define "ig.name" -}}inference-gateway{{- end -}}
{{- define "ig.labels" -}}
app.kubernetes.io/name: {{ include "ig.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}
{{- define "ig.selector" -}}
app.kubernetes.io/name: {{ include "ig.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
