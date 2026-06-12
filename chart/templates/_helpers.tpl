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
Whether to render the bundled VPA controller stack (CRDs, RBAC, recommender,
updater, admission-controller, webhook). Outputs "true" or "".

  verticalPodAutoscaler.installController:
    auto  (default) — install only if the cluster has no VPA API yet, OR we
                      already own the install (our vpa-recommender Deployment
                      exists in this namespace). The ownership check keeps the
                      stack rendered across upgrades after the Capabilities probe
                      flips to "present", so Helm keeps managing it instead of
                      deleting it.
    true            — always render/manage it.
    false           — never (you run VPA yourself).
*/}}
{{- define "weft-operator.installVpaController" -}}
{{- if .Values.verticalPodAutoscaler.enabled -}}
{{- $mode := .Values.verticalPodAutoscaler.installController -}}
{{- if kindIs "bool" $mode -}}
{{- if $mode }}true{{ end -}}
{{- else if eq (lower (toString $mode)) "auto" -}}
{{- $apiPresent := .Capabilities.APIVersions.Has "autoscaling.k8s.io/v1/VerticalPodAutoscaler" -}}
{{- $weOwn := not (empty (lookup "apps/v1" "Deployment" .Release.Namespace "vpa-recommender")) -}}
{{- if or (not $apiPresent) $weOwn }}true{{ end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
VPA admission webhook serving cert (base64 Secret data: caCert.pem / serverCert.pem
/ serverKey.pem). Reuses an existing in-cluster vpa-tls-certs Secret when present so
the CA stays stable across upgrades; mints a fresh self-signed CA + serving cert for
vpa-webhook.<ns>.svc otherwise.
*/}}
{{- define "weft-operator.vpaTlsCerts" -}}
{{- $existing := (lookup "v1" "Secret" .Release.Namespace "vpa-tls-certs") -}}
{{- if and $existing $existing.data (index $existing.data "serverCert.pem") -}}
caCert.pem: {{ index $existing.data "caCert.pem" }}
serverCert.pem: {{ index $existing.data "serverCert.pem" }}
serverKey.pem: {{ index $existing.data "serverKey.pem" }}
{{- else -}}
{{- $ns := .Release.Namespace -}}
{{- $cn := printf "vpa-webhook.%s.svc" $ns -}}
{{- $altNames := list $cn (printf "vpa-webhook.%s.svc.cluster.local" $ns) "vpa-webhook" -}}
{{- $ca := genCA "vpa-webhook-ca" 3650 -}}
{{- $cert := genSignedCert $cn nil $altNames 3650 $ca -}}
caCert.pem: {{ $ca.Cert | b64enc }}
serverCert.pem: {{ $cert.Cert | b64enc }}
serverKey.pem: {{ $cert.Key | b64enc }}
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
