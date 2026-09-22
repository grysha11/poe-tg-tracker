{{/* Chart name, truncated to the 63-char label limit. */}}
{{- define "poe-tracker.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Full resource name. A release called "poe-tracker" yields "poe-tracker",
not "poe-tracker-poe-tracker".
*/}}
{{- define "poe-tracker.fullname" -}}
{{- if contains .Chart.Name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/* Labels for every resource. */}}
{{- define "poe-tracker.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "poe-tracker.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels. Keep this set minimal and stable: a Deployment's selector
is immutable, so changing these later forces deleting the Deployment.
*/}}
{{- define "poe-tracker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "poe-tracker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
