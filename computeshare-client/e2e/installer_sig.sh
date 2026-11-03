#!/usr/bin/env bash
# Card 012 acceptance: a signed release installs; a tampered binary, a tampered SHA256SUMS,
# a missing signature and an installer built without a key all abort with the R6 message.
# Needs: go, openssl, python3. No root (installs into a temp dir). Usage: e2e/installer_sig.sh
set -u
cd "$(dirname "$0")/.."
export NO_PROXY='*' no_proxy='*'
d=$(mktemp -d); trap 'kill $HP 2>/dev/null; rm -rf $d' EXIT
pass=0; fail=0; t(){ if grep -q "$2" <<<"$3"; then echo "PASS $1"; pass=$((pass+1)); else echo "FAIL $1"; echo "$3" | tail -3; fail=$((fail+1)); fi; }
R6="امضای نسخه معتبر نیست"
cp -r . $d/src && cd $d/src
go build -o $d/cs ./cmd/cs || exit 1
printf 'e2e passphrase 012\ne2e passphrase 012\n' | $d/cs release keygen --dir $d/k >/dev/null 2>&1
cp $d/k/release.pub internal/release/release.pub
make dist VERSION=0.0.0-e2e >/dev/null 2>&1 || { echo "make dist failed"; exit 1; }
printf 'e2e passphrase 012\n' | $d/cs release sign --key $d/k/release.key dist/SHA256SUMS >/dev/null 2>&1
cd dist; python3 -m http.server 18777 --bind 127.0.0.1 >/dev/null 2>&1 & HP=$!; sleep 1
run(){ mkdir -p $d/bin; CS_NO_SETUP=1 CS_PREFIX=$d/bin sh "${1:-install.sh}" --from http://127.0.0.1:18777 2>&1; }
t "signed release installs" "installed: csnode" "$(run)"
t "bench helper installed" "cs-bench-linux-amd64" "$(ls $d/bin)"
cp csnode-linux-amd64 $d/b; echo junk >> csnode-linux-amd64
t "tampered binary aborts" "$R6" "$(run)"; cp $d/b csnode-linux-amd64
cp SHA256SUMS $d/s; sed -i '1s/^./0/' SHA256SUMS
t "tampered SHA256SUMS aborts" "$R6" "$(run)"; cp $d/s SHA256SUMS
mv SHA256SUMS.sig $d/sig
t "missing signature aborts" "$R6" "$(run)"; mv $d/sig SHA256SUMS.sig
sed '/^RELEASE_PUB=/,/^-----END PUBLIC KEY-----"$/c RELEASE_PUB=""' install.sh > $d/unsigned.sh
t "installer without key aborts" "$R6" "$(run $d/unsigned.sh)"
echo "== $pass passed, $fail failed"; [ $fail = 0 ]
