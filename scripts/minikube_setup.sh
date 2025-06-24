#!/bin/bash
#
# Script to set up a Minikube cluster for ClowsControl development.
#
# Prerequisites:
# - Minikube installed (https://minikube.sigs.k8s.io/docs/start/)
# - kubectl installed (https://kubernetes.io/docs/tasks/tools/install-kubectl/)
# - A container runtime compatible with Minikube (e.g., Docker, Podman)

set -e # Exit immediately if a command exits with a non-zero status.
set -u # Treat unset variables as an error when substituting.
set -o pipefail # Return value of a pipeline is the value of the last command to exit with a non-zero status

# --- Configuration ---
MINIKUBE_PROFILE="clowd-control-dev"
K8S_NAMESPACE="clowd-control-dev-ns"
NODE_LABEL_KEY="clowder.io/namespace"

# --- Helper Functions ---
info() {
    echo "[INFO] $1"
}

error_exit() {
    echo "[ERROR] $1" >&2
    exit 1
}

# --- Main Logic ---
info "Starting Minikube cluster with profile '${MINIKUBE_PROFILE}'..."
if ! minikube start -p "${MINIKUBE_PROFILE}"; then
    error_exit "Failed to start Minikube. Please check Minikube installation and logs."
fi

info "Minikube cluster '${MINIKUBE_PROFILE}' started."

info "Enabling Minikube registry addon..."
if ! minikube -p "${MINIKUBE_PROFILE}" addons enable registry; then
    info "Failed to enable registry addon. Continuing without it."
fi

info "Setting kubectl context to '${MINIKUBE_PROFILE}'..."
if ! kubectl config use-context "${MINIKUBE_PROFILE}"; then
    error_exit "Failed to set kubectl context to '${MINIKUBE_PROFILE}'. Please check kubectl configuration."
fi

info "Creating Kubernetes namespace '${K8S_NAMESPACE}' if it doesn't exist..."
if ! kubectl get namespace "${K8S_NAMESPACE}" > /dev/null 2>&1; then
    if ! kubectl create namespace "${K8S_NAMESPACE}"; then
        error_exit "Failed to create namespace '${K8S_NAMESPACE}'."
    fi
    info "Namespace '${K8S_NAMESPACE}' created."
else
    info "Namespace '${K8S_NAMESPACE}' already exists."
fi

info "Labeling Minikube node(s) with '${NODE_LABEL_KEY}=${K8S_NAMESPACE}'..."
# Get the name of the node(s) in the Minikube profile. Usually just one for Minikube.
NODE_NAMES=$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}')
if [ -z "$NODE_NAMES" ]; then
    error_exit "Could not find any nodes in the Minikube cluster '${MINIKUBE_PROFILE}'."
fi

for NODE_NAME in $NODE_NAMES; do
    info "Labeling node '${NODE_NAME}'..."
    if ! kubectl label node "${NODE_NAME}" "${NODE_LABEL_KEY}=${K8S_NAMESPACE}" --overwrite; then
        error_exit "Failed to label node '${NODE_NAME}'."
    fi
done
info "Minikube node(s) labeled successfully."

info "To use Minikube's Docker daemon (optional, for building images directly into Minikube):"
info "Run: eval \$(minikube -p ${MINIKUBE_PROFILE} docker-env)"
info "To switch back to your host's Docker daemon:"
info "Run: eval \$(minikube -p ${MINIKUBE_PROFILE} docker-env -u)"
info ""
info "Minikube setup complete for profile '${MINIKUBE_PROFILE}' and namespace '${K8S_NAMESPACE}'."
info "The KubernetesNodeProvider should be configured to use namespace: '${K8S_NAMESPACE}'."
