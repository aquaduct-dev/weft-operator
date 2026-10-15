#!/bin/bash

HOOK_FILE=".git/hooks/pre-commit"

echo "Setting up pre-commit hook at $HOOK_FILE..."

cat > "$HOOK_FILE" << 'EOF'
#!/bin/bash
set -e

echo "Running gazelle to fix build files..."
bazel run //:gazelle

echo "Running bazel tests..."
# Same filter as CI: the firecracker e2e tests need --config=kvm.
bazel test --build_tests_only --test_tag_filters=-requires-kvm //...
EOF

chmod +x "$HOOK_FILE"

echo "Pre-commit hook installed successfully."
