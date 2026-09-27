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

{{- define "poe-tracker.componentSelectorLabels" -}}
app.kubernetes.io/name: {{ include "poe-tracker.name" .root }}-{{ .name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end }}

{{- define "poe-tracker.component" -}}
{{- $root := .root -}}
{{- $fullname := printf "%s-%s" (include "poe-tracker.fullname" $root) .name -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ $fullname }}
  labels:
    {{- include "poe-tracker.labels" $root | nindent 4 }}
spec:
  replicas: {{ .values.replicas }}
  selector:
    matchLabels:
      {{- include "poe-tracker.componentSelectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "poe-tracker.componentSelectorLabels" . | nindent 8 }}
      annotations:
        checksum/config: {{ include (print $root.Template.BasePath "/configmap.yaml") $root | sha256sum }}
    spec:
      {{- with $root.Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ .name }}
          image: "{{ $root.Values.image.repository }}:{{ required "image.tag is required" $root.Values.image.tag }}"
          imagePullPolicy: {{ $root.Values.image.pullPolicy }}
          command: ["./{{ .name }}"]
          env:
            - name: {{ upper .protocol }}_LISTEN_ADDR
              value: ":{{ .values.port }}"
          envFrom:
            - configMapRef:
                name: {{ include "poe-tracker.fullname" $root }}-config
            {{- if .secret }}
            - secretRef:
                name: {{ required "existingSecret is required" $root.Values.existingSecret }}
            {{- end }}
          ports:
            - name: {{ .protocol }}
              containerPort: {{ .values.port }}
          {{- if eq .protocol "grpc" }}
          readinessProbe:
            grpc:
              port: {{ .values.port }}
            periodSeconds: 10
            failureThreshold: 3
          livenessProbe:
            grpc:
              port: {{ .values.port }}
            periodSeconds: 20
            failureThreshold: 3
          {{- else }}
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            periodSeconds: 10
            failureThreshold: 3
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            periodSeconds: 20
            failureThreshold: 3
          {{- end }}
          {{- with $root.Values.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ $fullname }}
  labels:
    {{- include "poe-tracker.labels" $root | nindent 4 }}
spec:
  selector:
    {{- include "poe-tracker.componentSelectorLabels" . | nindent 4 }}
  ports:
    - name: {{ .protocol }}
      port: {{ .values.port }}
      targetPort: {{ .protocol }}
{{- end }}
