{{/*
Expand the name of the chart.
*/}}
{{- define "weft-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "weft-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "weft-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "weft-operator.labels" -}}
helm.sh/chart: {{ include "weft-operator.chart" . }}
{{ include "weft-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "weft-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "weft-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Honeypot ingest Secret name. Prefer .Values.honeypot.existingSecret if set;
otherwise default to "<release>-honeypot-ingest".
*/}}
{{- define "weft-operator.honeypotSecretName" -}}
{{- if .Values.honeypot.existingSecret }}
{{- .Values.honeypot.existingSecret }}
{{- else }}
{{- printf "%s-honeypot-ingest" (include "weft-operator.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Honeypot ingest secret value. Reuses an existing in-cluster Secret value when
present (so upgrades don't rotate the secret out from under the consumer),
otherwise generates a 32-char random string on first install.
*/}}
{{- define "weft-operator.honeypotSecretValue" -}}
{{- $existing := (lookup "v1" "Secret" .Release.Namespace (include "weft-operator.honeypotSecretName" .)) -}}
{{- if and $existing $existing.data $existing.data.secret -}}
{{- index $existing.data "secret" | b64dec -}}
{{- else -}}
{{- randAlphaNum 32 -}}
{{- end -}}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "weft-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "weft-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
