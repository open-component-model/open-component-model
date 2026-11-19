{{/*
Expand the name of the chart.
*/}}
{{- define "ocm-k8s-toolkit.name" -}}
{{- default "ocm-k8s-toolkit" .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
The name, by default, is ocm-k8s-toolkit. It can be overwritten by nameOverride or fullnameOverride.
*/}}
{{- define "ocm-k8s-toolkit.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "ocm-k8s-toolkit" .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Resource name with proper truncation for Kubernetes 63-character limit.
Takes a dict with:
  - .suffix: Resource name suffix (e.g., "metrics", "webhook")
  - .context: Template context (root context with .Values, .Release, etc.)
Dynamically calculates safe truncation to ensure total name length <= 63 chars.
*/}}
{{- define "ocm-k8s-toolkit.resourceName" -}}
{{- $fullname := include "ocm-k8s-toolkit.fullname" .context }}
{{- $suffix := .suffix }}
{{- $maxLen := sub 62 (len $suffix) | int }}
{{- if gt (len $fullname) $maxLen }}
{{- printf "%s-%s" (trunc $maxLen $fullname | trimSuffix "-") $suffix | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" $fullname $suffix | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Port the manager serves metrics on, parsed from manager.metricsServer.bindAddress.
Empty when metrics are disabled ("0", the manager default). dig keeps a helm
upgrade --reuse-values from a release without the key working.
*/}}
{{- define "ocm-k8s-toolkit.metricsPort" -}}
{{- $addr := dig "metricsServer" "bindAddress" "0" .Values.manager | toString }}
{{- if ne $addr "0" }}{{ regexFind "[0-9]+$" $addr }}{{ end }}
{{- end }}

{{/*
Same as metricsPort, but fails the render when metrics are disabled. Used by
the Prometheus objects, which are useless without a metrics endpoint.
*/}}
{{- define "ocm-k8s-toolkit.prometheusMetricsPort" -}}
{{- $port := include "ocm-k8s-toolkit.metricsPort" . }}
{{- if not $port }}
{{- fail "prometheus.enable=true requires manager.metricsServer.bindAddress to be set (for example \":8443\"); the manager serves no metrics with the default \"0\"" }}
{{- end }}
{{- $port }}
{{- end }}
