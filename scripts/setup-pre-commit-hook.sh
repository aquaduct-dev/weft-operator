#!/bin/bash

HOOK_FILE=".git/hooks/pre-commit"

echo "Setting up pre-commit hook at $HOOK_FILE..."

cat > "$HOOK_FILE" << 'EOF'
#!/bin/bash
set -e

echo "Running gazelle to fix build files..."
bazel run //:gazelle

echo "Running bazel tests..."
bazel test //...
EOF

chmod +x "$HOOK_FILE"

echo "Pre-commit hook installed successfully."
