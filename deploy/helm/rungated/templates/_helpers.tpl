{{- define "rungated.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "rungated.fullname" -}}
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

{{- define "rungated.labels" -}}
app.kubernetes.io/name: {{ include "rungated.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "rungated.selectorLabels" -}}
app.kubernetes.io/name: {{ include "rungated.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "rungated.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "rungated.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "rungated.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "rungated.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "rungated.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- if .Values.onnx.enabled -}}
{{- printf "%s:%s-onnx" .Values.image.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{- define "rungated.hasChartSecret" -}}
{{- if and (not .Values.secrets.existingSecret) (or .Values.secrets.apiKey .Values.secrets.masterKey .Values.secrets.databaseUrl .Values.secrets.openaiApiKey .Values.secrets.anthropicApiKey .Values.secrets.slackBotToken .Values.secrets.slackSigningSecret .Values.secrets.clickhousePassword) -}}true{{- end -}}
{{- end -}}

{{- define "rungated.validate" -}}
{{- if and (gt (int .Values.replicaCount) 1) (eq .Values.config.store.kind "embedded") -}}
{{- fail "replicaCount above 1 requires config.store.kind postgres, the embedded store is single writer" -}}
{{- end -}}
{{- if and .Values.autoscaling.enabled (eq .Values.config.store.kind "embedded") -}}
{{- fail "autoscaling requires config.store.kind postgres" -}}
{{- end -}}
{{- if and .Values.tier1.enabled (not .Values.onnx.enabled) -}}
{{- fail "tier1.enabled requires onnx.enabled so the image contains ONNX Runtime" -}}
{{- end -}}
{{- if and .Values.tier1.enabled (not .Values.tier1.modelsClaim) -}}
{{- fail "tier1.enabled requires tier1.modelsClaim, a PersistentVolumeClaim holding the model files" -}}
{{- end -}}
{{- if and (eq .Values.config.store.kind "postgres") (not .Values.secrets.databaseUrl) (not .Values.secrets.existingSecret) -}}
{{- fail "config.store.kind postgres requires secrets.databaseUrl or secrets.existingSecret with the key RUNGATED_DATABASE_URL" -}}
{{- end -}}
{{- end -}}

{{- define "rungated.config" -}}
listen: ":8080"
egress_listen: ":8081"
data_dir: /data
manifests:
  - /etc/rungated/manifests
{{- if .Values.tier1.enabled }}
tier1:
  enabled: true
  library_path: {{ .Values.tier1.libraryPath | quote }}
  classifier_path: {{ .Values.tier1.classifierPath | quote }}
  tokenizer_path: {{ .Values.tier1.tokenizerPath | quote }}
  {{- if .Values.tier1.nerPath }}
  ner_path: {{ .Values.tier1.nerPath | quote }}
  {{- end }}
  {{- if .Values.tier1.nerTokenizerPath }}
  ner_tokenizer_path: {{ .Values.tier1.nerTokenizerPath | quote }}
  {{- end }}
  {{- if .Values.tier1.nerLabelsPath }}
  ner_labels_path: {{ .Values.tier1.nerLabelsPath | quote }}
  {{- end }}
  max_len: {{ .Values.tier1.maxLen }}
  threshold: {{ .Values.tier1.threshold }}
  temperature: {{ .Values.tier1.temperature }}
  timeout_ms: {{ .Values.tier1.timeoutMs }}
{{- end }}
{{ toYaml .Values.config }}
{{- end -}}
