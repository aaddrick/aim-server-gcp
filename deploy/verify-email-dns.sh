#!/usr/bin/env bash
# verify-email-dns.sh <domain>
#
# Verifies the Resend email-authentication DNS records for <domain>
# (the domain registered in the Resend dashboard, e.g. aim.example.com).
# Adapted from flyspacea's checker.
#
# Exit codes: 0 = all checks passed, 1 = one or more failed.
set -euo pipefail

DOMAIN="${1:?usage: verify-email-dns.sh <domain>}"
PASS=0
FAIL=0

command -v dig >/dev/null || { echo "error: dig is required (apt install dnsutils / dnf install bind-utils)" >&2; exit 1; }

if [ -t 1 ]; then
  GREEN='\033[0;32m'; RED='\033[0;31m'; BOLD='\033[1m'; RESET='\033[0m'
else
  GREEN=''; RED=''; BOLD=''; RESET=''
fi

pass() { PASS=$((PASS + 1)); printf "%b  ✓ %s%b\n" "$GREEN" "$1" "$RESET"; }
fail() { FAIL=$((FAIL + 1)); printf "%b  ✗ %s%b\n" "$RED" "$1" "$RESET"; }
header() { printf "\n%b%s%b\n" "$BOLD" "$1" "$RESET"; }

header "DKIM (resend._domainkey.${DOMAIN})"
DKIM=$(dig +short TXT "resend._domainkey.${DOMAIN}" | tr -d '"' || true)
if [[ "$DKIM" == p=* ]]; then
  pass "DKIM record present"
else
  fail "DKIM record missing or malformed (got: '${DKIM:-nothing}')"
fi

header "SPF (send.${DOMAIN})"
SPF=$(dig +short TXT "send.${DOMAIN}" | tr -d '"' || true)
if [[ "$SPF" == *"v=spf1"* && "$SPF" == *"include:amazonses.com"* ]]; then
  pass "SPF includes amazonses.com"
else
  fail "SPF record missing or wrong (got: '${SPF:-nothing}')"
fi

header "MX (send.${DOMAIN})"
MX=$(dig +short MX "send.${DOMAIN}" || true)
if [[ "$MX" == *"amazonses.com."* ]]; then
  pass "MX points at Amazon SES feedback host"
else
  fail "MX record missing or wrong (got: '${MX:-nothing}')"
fi

header "DMARC (_dmarc.${DOMAIN})"
DMARC=$(dig +short TXT "_dmarc.${DOMAIN}" | tr -d '"' || true)
if [[ "$DMARC" == *"v=DMARC1"* ]]; then
  pass "DMARC record present"
else
  fail "DMARC record missing (got: '${DMARC:-nothing}')"
fi

printf "\n%d passed, %d failed\n" "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
