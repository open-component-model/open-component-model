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
Port of a manager listen address (dict "addr" value "key" "values.path"), or empty
when the address is "" or "0", which the manager treats as disabled. Fails the
render unless the address is host:port with a decimal port in 1-65535; a bracketed
IPv6 host may carry a zone ("[fe80::1%eth0]:8443"). It checks the shape only, not
that the host resolves. atoi reads the digits as decimal like the manager does; int
would read ":08081" as octal.
*/}}
{{- define "ocm-k8s-toolkit.portFromAddress" -}}
{{- $addr := .addr | toString }}
{{- if and $addr (ne $addr "0") }}
{{- if not (mustRegexMatch `^(\[[0-9A-Fa-f:.]+(%[^\]]+)?\]|[^:\[\]]*):[0-9]+$` $addr) }}
{{- fail (printf "%s %q must be host:port, for example \":8443\", or \"0\" to disable" .key $addr) }}
{{- end }}
{{- $port := regexFind "[0-9]+$" $addr | atoi }}
{{- if or (lt $port 1) (gt $port 65535) }}
{{- fail (printf "%s %q has port %d outside 1-65535" .key $addr $port) }}
{{- end }}
{{- $port }}
{{- end }}
{{- end }}

{{/*
Port the manager serves metrics on, parsed from manager.metricsServer.bindAddress.
Empty when metrics are disabled ("0", the manager default). dig keeps a helm
upgrade --reuse-values from a release without the key working.
*/}}
{{- define "ocm-k8s-toolkit.metricsPort" -}}
{{- include "ocm-k8s-toolkit.portFromAddress" (dict "addr" (dig "metricsServer" "bindAddress" "0" .Values.manager) "key" "manager.metricsServer.bindAddress") }}
{{- end }}

{{/*
Port the manager serves health probes on. Never empty: the liveness and readiness
probes and the NetworkPolicy ingress rule need it.
*/}}
{{- define "ocm-k8s-toolkit.healthProbePort" -}}
{{- $port := include "ocm-k8s-toolkit.portFromAddress" (dict "addr" (dig "healthProbe" "bindAddress" ":8081" .Values.manager | default ":8081") "key" "manager.healthProbe.bindAddress") }}
{{- if not $port }}
{{- fail "manager.healthProbe.bindAddress must not be \"0\"; the liveness and readiness probes need the health endpoint" }}
{{- end }}
{{- $port }}
{{- end }}

{{/*
Fails the render when two manager listeners share a port: the second one cannot bind
and the manager exits at startup. Called from manager/manager.yaml, which always
renders, so the address checks in healthProbePort and metricsPort run on every
install. 9443 is the webhook server port, see manager/manager.yaml.
*/}}
{{- define "ocm-k8s-toolkit.validatePorts" -}}
{{- $listeners := list (list "manager.healthProbe.bindAddress" (include "ocm-k8s-toolkit.healthProbePort" .)) }}
{{- with include "ocm-k8s-toolkit.metricsPort" . }}
{{- $listeners = append $listeners (list "manager.metricsServer.bindAddress" .) }}
{{- end }}
{{- if .Values.webhook.enable }}
{{- $listeners = append $listeners (list "the webhook server" "9443") }}
{{- end }}
{{- $seen := dict }}
{{- range $listeners }}
{{- $name := index . 0 }}
{{- $port := index . 1 }}
{{- with get $seen $port }}
{{- fail (printf "%s port %s collides with %s; each manager listener needs its own port" $name $port .) }}
{{- end }}
{{- $_ := set $seen $port $name }}
{{- end }}
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
