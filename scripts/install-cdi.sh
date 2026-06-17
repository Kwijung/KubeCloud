#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

require_kubectl

CDI_VERSION_EXPLICIT=false
if [ "${CDI_VERSION+x}" = "x" ]; then
  CDI_VERSION_EXPLICIT=true
fi

if kubectl -n cdi get cdi cdi >/dev/null 2>&1; then
  if [ "$CDI_VERSION_EXPLICIT" = "false" ]; then
    info "CDI is already installed; set CDI_VERSION to force manifest reconciliation"
    info "Current CDI status:"
    kubectl -n cdi get cdi cdi -o wide || warn "Failed to print current CDI status"
    info "CDI pods:"
    kubectl get pods -n cdi -o wide || warn "Failed to list CDI pods"
    exit 0
  fi
  warn "CDI is already installed; applying manifests for CDI_VERSION may upgrade or reconcile the installation"
fi

require_cmd sed
if [ "$CDI_VERSION_EXPLICIT" = "true" ]; then
  CDI_VERSION="${CDI_VERSION:-}"
else
  require_cmd curl
  CDI_VERSION="$(
    curl -fsSL https://api.github.com/repos/kubevirt/containerized-data-importer/releases/latest \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p'
  )" || die "Failed to fetch latest CDI release tag"
fi
CDI_VERSION="$(printf '%s' "$CDI_VERSION" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
if [ -z "$CDI_VERSION" ]; then
  if [ "$CDI_VERSION_EXPLICIT" = "true" ]; then
    die "CDI_VERSION is set but empty"
  fi
  die "Unable to determine CDI version from latest release response"
fi
[[ "$CDI_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9._-]+)?$ ]] \
  || die "Invalid CDI version: $CDI_VERSION"

CDI_OPERATOR_URL="https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-operator.yaml"
CDI_CR_URL="https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-cr.yaml"
CDI_OPERATOR_WAIT_TIMEOUT="${CDI_WAIT_TIMEOUT:-300s}"
CDI_RESOURCE_WAIT_TIMEOUT="${CDI_WAIT_TIMEOUT:-600s}"

info "Installing CDI version: $CDI_VERSION"
info "CDI operator URL: $CDI_OPERATOR_URL"
info "CDI CR URL: $CDI_CR_URL"

kubectl apply -f "$CDI_OPERATOR_URL" \
  || die "Failed to apply CDI operator manifest for $CDI_VERSION"
kubectl apply -f "$CDI_CR_URL" \
  || die "Failed to apply CDI CR manifest for $CDI_VERSION"

info "Waiting for CDI operator deployment to become Available"
kubectl -n cdi wait deployment/cdi-operator --for=condition=Available --timeout="$CDI_OPERATOR_WAIT_TIMEOUT" \
  || die "CDI operator deployment did not become Available within $CDI_OPERATOR_WAIT_TIMEOUT"

info "Waiting for CDI to become Available"
kubectl -n cdi wait cdi/cdi --for=condition=Available --timeout="$CDI_RESOURCE_WAIT_TIMEOUT" \
  || die "CDI did not become Available within $CDI_RESOURCE_WAIT_TIMEOUT"

info "CDI pods:"
kubectl get pods -n cdi -o wide || die "Failed to list CDI pods"

info "CDI installation completed"
