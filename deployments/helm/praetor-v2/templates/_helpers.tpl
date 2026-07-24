{{/* Chart name (overridable). */}}
{{- define "praetor.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully-qualified release name. */}}
{{- define "praetor.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "praetor.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "praetor.labels" -}}
helm.sh/chart: {{ include "praetor.chart" . }}
{{ include "praetor.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: praetor
{{- end -}}

{{/* Selector labels (release-scoped). */}}
{{- define "praetor.selectorLabels" -}}
app.kubernetes.io/name: {{ include "praetor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Service account name. */}}
{{- define "praetor.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "praetor.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* Secret name (existing or chart-managed). */}}
{{- define "praetor.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- printf "%s-secrets" (include "praetor.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "praetor.configMapName" -}}
{{- printf "%s-config" (include "praetor.fullname" .) -}}
{{- end -}}

{{/*
Resolve a service image reference.
Usage: include "praetor.image" (dict "root" $ "svc" "api")
*/}}
{{- define "praetor.image" -}}
{{- $root := .root -}}
{{- $repo := index $root.Values.images .svc -}}
{{- $componentTag := index $root.Values.imageTags .svc -}}
{{- $componentDigest := index ($root.Values.imageDigests | default dict) .svc -}}
{{- $tag := $root.Values.image.tag | default $componentTag | default $root.Chart.AppVersion -}}
{{- $registry := $root.Values.image.registry -}}
{{- if hasKey ($root.Values.imageRegistries | default dict) .svc -}}
{{- $registry = index $root.Values.imageRegistries .svc -}}
{{- end -}}
{{- if $registry -}}
{{- if $componentDigest -}}
{{- printf "%s/%s@%s" $registry $repo $componentDigest -}}
{{- else -}}
{{- printf "%s/%s:%s" $registry $repo $tag -}}
{{- end -}}
{{- else -}}
{{- if $componentDigest -}}
{{- printf "%s@%s" $repo $componentDigest -}}
{{- else -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* DATABASE_URL: external wins, else the bundled Postgres service. */}}
{{- define "praetor.databaseUrl" -}}
{{- if .Values.database.external.url -}}
{{- .Values.database.external.url -}}
{{- else -}}
{{- printf "postgres://%s:%s@%s-postgresql:5432/%s?sslmode=disable" .Values.database.bundled.user .Values.database.bundled.password (include "praetor.fullname" .) .Values.database.bundled.database -}}
{{- end -}}
{{- end -}}

{{/* NATS_URL: external wins, else the bundled NATS service. */}}
{{- define "praetor.natsUrl" -}}
{{- if .Values.nats.external.url -}}
{{- .Values.nats.external.url -}}
{{- else -}}
{{- printf "nats://%s-nats:4222" (include "praetor.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/* Host-runner callback URL: explicit value wins, else derive from the ingress. */}}
{{- define "praetor.callbackUrl" -}}
{{- if .Values.hostRunner.callbackUrl -}}
{{- .Values.hostRunner.callbackUrl -}}
{{- else if and .Values.ingress.enabled .Values.ingress.ingestionHost -}}
{{- printf "https://%s" .Values.ingress.ingestionHost -}}
{{- end -}}
{{- end -}}

{{/* Bundled NATS max_file_store expressed in MB (parses GB/MB/GiB/MiB; 0 if unknown). */}}
{{- define "praetor.natsMaxFileStoreMB" -}}
{{- $v := .Values.nats.bundled.maxFileStore -}}
{{- if hasSuffix "GB" $v -}}{{ mul (trimSuffix "GB" $v | int) 1024 }}
{{- else if hasSuffix "GiB" $v -}}{{ mul (trimSuffix "GiB" $v | int) 1024 }}
{{- else if hasSuffix "MB" $v -}}{{ trimSuffix "MB" $v | int }}
{{- else if hasSuffix "MiB" $v -}}{{ trimSuffix "MiB" $v | int }}
{{- else -}}0{{- end -}}
{{- end -}}

{{/* Host used for waiting/probing the bundled Postgres. */}}
{{- define "praetor.postgresHost" -}}
{{- printf "%s-postgresql" (include "praetor.fullname" .) -}}
{{- end -}}

{{/*
initContainer that blocks until DB migrations are applied (the `organizations`
table exists — created by migration 000001 and used by the migrator as its
baseline marker). Reused by every service that reads the schema.
*/}}
{{- define "praetor.waitForMigrations" -}}
- name: wait-for-migrations
  image: {{ .Values.database.bundled.image | default "postgres:15" }}
  imagePullPolicy: {{ .Values.image.pullPolicy }}
  command:
    - sh
    - -c
    - |
      until psql "$DATABASE_URL" -tAc "SELECT to_regclass('public.organizations')" 2>/dev/null | grep -q organizations; do
        echo "waiting for migrations (organizations table)..."; sleep 3
      done
      echo "schema ready."
  env:
    - name: DATABASE_URL
      valueFrom:
        secretKeyRef:
          name: {{ include "praetor.secretName" . }}
          key: DATABASE_URL
{{- end -}}
