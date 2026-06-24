#!/usr/bin/env bash

set -e

# Load environment variables from .env file if it exists
if [ -f .env ]; then
  echo "Loading environment variables from .env..."
  while IFS= read -r line || [ -n "$line" ]; do
    # Skip comments and empty lines
    if [[ ! "$line" =~ ^[[:space:]]*# ]] && [[ "$line" =~ = ]]; then
      # Extract key and value, stripping surrounding quotes
      key=$(echo "$line" | cut -d'=' -f1 | xargs)
      value=$(echo "$line" | cut -d'=' -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")
      export "$key"="$value"
    fi
  done < .env
fi

# Generate GITHUB_TOKEN using get-token utility if not already set
if [ -z "$GITHUB_TOKEN" ]; then
  echo "GITHUB_TOKEN is not set. Attempting to generate a token using get-token utility..."
  if TOKEN=$(go run ./cmd/get-token 2>/dev/null); then
    export GITHUB_TOKEN="$TOKEN"
    echo "Successfully generated and set GITHUB_TOKEN."
  else
    echo "Warning: get-token utility failed to generate token. You may need to set GITHUB_TOKEN manually."
  fi
fi

# Cleanup function to kill delve on exit
DLV_PID=""
cleanup() {
  if [ -n "$DLV_PID" ]; then
    echo "Cleaning up debugger (PID $DLV_PID)..."
    kill "$DLV_PID" 2>/dev/null || true
  fi
  rm -f dlv.log
  rm -f ./debug_bin
  rm -f ./__debug_bin*
}
trap cleanup EXIT INT TERM

# Start Delve headless (compiles and runs the provider in debug mode)
echo "Starting Delve debugger in headless mode..."
dlv debug --output ./debug_bin --headless --listen=:2345 --api-version=2 --accept-multiclient --continue --log -- -debug > dlv.log 2>&1 &
DLV_PID=$!

# Wait for the reattach env var to be printed by the provider
echo "Waiting for provider to initialize (this compiles the code, it may take a moment)..."
TF_REATTACH=""
# Wait up to 60 seconds (120 * 0.5s)
for i in {1..120}; do
  printf "."
  if grep -q "TF_REATTACH_PROVIDERS=" dlv.log; then
    echo "" # New line after dots
    # Extract the line containing TF_REATTACH_PROVIDERS
    LINE=$(grep "TF_REATTACH_PROVIDERS=" dlv.log)
    # Extract the JSON content inside the single quotes
    TF_REATTACH=$(echo "$LINE" | sed -n "s/.*TF_REATTACH_PROVIDERS='\(.*\)'.*/\1/p")
    break
  fi
  sleep 0.5
done

if [ -z "$TF_REATTACH" ]; then
  echo ""
  echo "Error: Failed to start provider or extract reattach configuration within 60s."
  cat dlv.log
  exit 1
fi

echo "=============================================================="
echo "Debugger is listening on port 2345"
echo "1. In VS Code, run 'Attach to Headless Debugger' (F5)."
echo "2. Return here and press [ENTER] to run Terraform."
echo "=============================================================="
read -r

export TF_REATTACH_PROVIDERS="$TF_REATTACH"

# Run terraform in the target example directory (defaults to examples/resources/ghapp_installation)
TARGET_DIR="${TF_EXAMPLE_DIR:-examples/resources/ghapp_installation}"
echo "Changing directory to $TARGET_DIR"
cd "$TARGET_DIR"

echo "Running: terraform $@"
terraform "$@"
