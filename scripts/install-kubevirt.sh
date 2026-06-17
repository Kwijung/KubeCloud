#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

require_kubectl

KUBEVIRT_VERSION_EXPLICIT=false
if [ "${KUBEVIRT_VERSION+x}" = "x" ]; then
  KUBEVIRT_VERSION_EXPLICIT=true
fi

if kubectl -n kubevirt get kv kubevirt >/dev/null 2>&1; then
  if [ "$KUBEVIRT_VERSION_EXPLICIT" = "false" ]; then
    info "KubeVirt is already installed; set KUBEVIRT_VERSION to force manifest reconciliation"
    info "Current KubeVirt status:"
    kubectl -n kubevirt get kv kubevirt -o wide || warn "Failed to print current KubeVirt status"
    info "KubeVirt pods:"
    kubectl get pods -n kubevirt -o wide || warn "Failed to list KubeVirt pods"
    exit 0
  fi
  warn "KubeVirt is already installed; applying manifests for KUBEVIRT_VERSION may upgrade or reconcile the installation"
fi

if [ "$KUBEVIRT_VERSION_EXPLICIT" = "true" ]; then
  KUBEVIRT_VERSION="${KUBEVIRT_VERSION:-}"
else
  require_cmd curl
  KUBEVIRT_VERSION="$(curl -fsSL https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)" \
    || die "Failed to fetch stable KubeVirt version"
fi
KUBEVIRT_VERSION="$(printf '%s' "$KUBEVIRT_VERSION" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
[ -n "$KUBEVIRT_VERSION" ] || die "Unable to determine KubeVirt version"
[[ "$KUBEVIRT_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9._-]+)?$ ]] \
  || die "Invalid KubeVirt version: $KUBEVIRT_VERSION"

KUBEVIRT_OPERATOR_URL="https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml"
KUBEVIRT_CR_URL="https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml"

info "Installing KubeVirt version: $KUBEVIRT_VERSION"
info "KubeVirt operator URL: $KUBEVIRT_OPERATOR_URL"
info "KubeVirt CR URL: $KUBEVIRT_CR_URL"

kubectl apply -f "$KUBEVIRT_OPERATOR_URL" \
  || die "Failed to apply KubeVirt operator manifest for $KUBEVIRT_VERSION"
kubectl apply -f "$KUBEVIRT_CR_URL" \
  || die "Failed to apply KubeVirt CR manifest for $KUBEVIRT_VERSION"

info "Waiting for KubeVirt to become Available"
kubectl -n kubevirt wait kv kubevirt --for=condition=Available --timeout="${KUBEVIRT_WAIT_TIMEOUT:-600s}" \
  || die "KubeVirt did not become Available within ${KUBEVIRT_WAIT_TIMEOUT:-600s}"

info "KubeVirt pods:"
kubectl get pods -n kubevirt -o wide || die "Failed to list KubeVirt pods"

info "KubeVirt installation completed"
