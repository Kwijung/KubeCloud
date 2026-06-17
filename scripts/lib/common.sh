#!/usr/bin/env bash

set -euo pipefail

info() {
  printf '[INFO] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*" >&2
}

die() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  [ "$#" -eq 1 ] && [ -n "$1" ] || die "require_cmd requires a command name"
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "Required command not found: $cmd"
}

require_kubectl() {
  require_cmd kubectl
  kubectl cluster-info >/dev/null 2>&1 || die "kubectl cannot reach the Kubernetes cluster"
}

server_version() {
  local version_json
  local version
  version_json="$(kubectl get --raw /version 2>/dev/null)" || die "Unable to retrieve Kubernetes server version"
  version="$(printf '%s\n' "$version_json" | sed -n 's/.*"gitVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  [ -n "$version" ] || die "Unable to extract Kubernetes gitVersion from /version"
  printf '%s\n' "$version"
}

require_namespace_absent_or_owned() {
  [ "$#" -eq 1 ] && [ -n "$1" ] || die "require_namespace_absent_or_owned requires a namespace"
  local namespace="$1"
  if kubectl get namespace "$namespace" >/dev/null 2>&1; then
    local managed
    managed="$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.labels.kloudia\.io/managed-by}' 2>/dev/null || true)"
    [ "$managed" = "infra-spike" ] || die "Namespace exists and is not owned by this spike: $namespace"
  fi
}

create_owned_namespace() {
  [ "$#" -eq 1 ] && [ -n "$1" ] || die "create_owned_namespace requires a namespace"
  local namespace="$1"
  require_namespace_absent_or_owned "$namespace"
  kubectl create namespace "$namespace" --dry-run=client -o yaml \
    | kubectl label --local -f - kloudia.io/managed-by=infra-spike -o yaml \
    | kubectl apply -f -
}

cleanup_namespace_on_success() {
  [ "$#" -eq 1 ] && [ -n "$1" ] || die "cleanup_namespace_on_success requires a namespace"
  local namespace="$1"
  if [ "${KLOUDIA_KEEP_RESOURCES:-false}" = "true" ]; then
    info "Keeping namespace because KLOUDIA_KEEP_RESOURCES=true: $namespace"
    return
  fi
  if ! kubectl get namespace "$namespace" >/dev/null 2>&1; then
    return
  fi
  local managed
  managed="$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.labels.kloudia\.io/managed-by}' 2>/dev/null || true)"
  [ "$managed" = "infra-spike" ] || die "Namespace exists and is not owned by this spike: $namespace"
  kubectl delete namespace "$namespace" --ignore-not-found=true >/dev/null
}
