#!/bin/bash
# Compare syft's parsed signature key IDs with rpm CLI output

IMAGE="${1:-registry.access.redhat.com/ubi8:latest}"

echo "Testing image: $IMAGE"
echo "========================================"

# Get syft output
SYFT_OUTPUT=$(cd /Users/willmurphy/work/syft && go run ./cmd/syft "$IMAGE" -o syft-json 2>/dev/null)

# Get list of RPM packages with their key IDs from syft
echo "Syft parsed signatures:"
echo "$SYFT_OUTPUT" | jq -r '
  .artifacts[]
  | select(.type == "rpm")
  | select(.metadata.signatures != null)
  | "\(.name): \(.metadata.signatures[0].issuer // "none")"
' | sort | head -20

echo ""
echo "RPM CLI signatures (from container):"
docker run --rm "$IMAGE" rpm -qa --qf '%{NAME}: %{RSAHEADER:pgpsig}\n' 2>/dev/null | \
  grep -v "^gpg-pubkey" | \
  sed 's/.*Key ID //' | \
  sort | head -20

echo ""
echo "========================================"
echo "Comparing first 10 packages..."
echo ""

# Compare specific packages
for pkg in bash glibc curl openssl-libs systemd rpm; do
  SYFT_KEY=$(echo "$SYFT_OUTPUT" | jq -r ".artifacts[] | select(.type == \"rpm\" and .name == \"$pkg\") | .metadata.signatures[0].issuer // \"none\"")
  RPM_KEY=$(docker run --rm "$IMAGE" rpm -q "$pkg" --qf '%{RSAHEADER:pgpsig}\n' 2>/dev/null | grep -oE '[0-9a-f]{16}$' || echo "not found")

  if [ "$SYFT_KEY" = "$RPM_KEY" ]; then
    STATUS="✓ MATCH"
  else
    STATUS="✗ MISMATCH"
  fi

  printf "%-20s syft=%-18s rpm=%-18s %s\n" "$pkg:" "$SYFT_KEY" "$RPM_KEY" "$STATUS"
done
