#!/bin/bash
#
# Script to stop and delete the Minikube cluster used for ClowdControl development.
#

set -e # Exit immediately if a command exits with a non-zero status.
set -u # Treat unset variables as an error when substituting.
set -o pipefail # Return value of a pipeline is the value of the last command to exit with a non-zero status

# --- Configuration ---
MINIKUBE_PROFILE="clowd-control-dev" # Must match the profile used in minikube_setup.sh

# --- Helper Functions ---
info() {
    echo "[INFO] $1"
}

error_exit() {
    echo "[ERROR] $1" >&2
    exit 1
}

# --- Main Logic ---
info "Attempting to stop Minikube cluster with profile '${MINIKUBE_PROFILE}'..."
if ! minikube stop -p "${MINIKUBE_PROFILE}"; then
    info "Failed to stop Minikube profile '${MINIKUBE_PROFILE}'. It might already be stopped or not exist."
else
    info "Minikube cluster '${MINIKUBE_PROFILE}' stopped."
fi

info "Attempting to delete Minikube cluster with profile '${MINIKUBE_PROFILE}'..."
if ! minikube delete -p "${MINIKUBE_PROFILE}"; then
    error_exit "Failed to delete Minikube profile '${MINIKUBE_PROFILE}'. Please check Minikube logs or delete manually."
fi

info "Minikube cluster '${MINIKUBE_PROFILE}' deleted successfully."
info "If you pointed your Docker client to Minikube's Docker daemon, you might want to unset it:"
info "Run: eval \$(minikube -p ${MINIKUBE_PROFILE} docker-env -u) (if the profile still existed, this might fail now)"
info "Or ensure your DOCKER_HOST environment variable is unset or points to your host's Docker daemon."
