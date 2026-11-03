#!/usr/bin/env bash
# Checks deploy/nginx/node.n3xus.ir.conf against a real nginx + csbroker on localhost (no root):
# path filter, method filter, WebSocket upgrade, panel login over HTTPS, XFF anti-spoofing.
# Needs: nginx, openssl, curl, go. Usage: e2e/nginx_check.sh
set -u
cd "$(dirname "$0")/.."; NGINX=${NGINX:-$(command -v nginx || echo /usr/sbin/nginx)}
d=$(mktemp -d); trap '$NGINX -c $d/nginx.conf -e $d/err.log -s stop 2>/dev/null; kill $BP 2>/dev/null; rm -rf $d' EXIT
pass=0; fail=0; t(){ [[ $2 == "$3" ]] && { echo "PASS $1"; pass=$((pass+1)); } || { echo "FAIL $1 (got $2, want $3)"; fail=$((fail+1)); }; }
go build -o $d/csbroker ./cmd/csbroker || exit 1
mkdir -p $d/certs; openssl req -x509 -newkey rsa:2048 -nodes -keyout $d/certs/k.pem -out $d/certs/c.pem -days 1 -subj /CN=node.n3xus.ir 2>/dev/null
sed "s#/etc/nginx/certs/node.n3xus.ir/fullchain.pem#$d/certs/c.pem#; s#/etc/nginx/certs/node.n3xus.ir/privkey.pem#$d/certs/k.pem#; s#172.18.0.1:8088#127.0.0.1:18089#; s/listen 443 ssl;/listen 18444 ssl;/; s/listen 18081;/listen 18081;/" deploy/nginx/node.n3xus.ir.conf > $d/site.conf
grep -q "allow 203.0.113.10; deny all;" $d/site.conf && t "panel allowlist present in sample" ok ok || t "panel allowlist present in sample" missing ok
sed -i "s/allow 203.0.113.10;/allow 127.0.0.1;/" $d/site.conf
printf 'pid %s/nginx.pid;\nerror_log %s/err.log;\nevents {}\nhttp {\naccess_log off;\nclient_body_temp_path %s;\nproxy_temp_path %s;\nfastcgi_temp_path %s;\nuwsgi_temp_path %s;\nscgi_temp_path %s;\ninclude %s/site.conf;\n}\n' $d $d $d $d $d $d $d $d > $d/nginx.conf
PW="Ng1nx-Chk-$(openssl rand -hex 6)"; printf '%s\n' "$PW" | $d/csbroker admin set-password --state $d/st >/dev/null 2>&1
$d/csbroker serve --state $d/st --config /dev/null --http 127.0.0.1:18089 --ssh 127.0.0.1:12297 --trusted-proxies 127.0.0.1/32 >$d/broker.log 2>&1 & BP=$!
$NGINX -c $d/nginx.conf -e $d/err.log || exit 1; sleep 1
U=https://node.n3xus.ir:18444; R=(--resolve node.n3xus.ir:18444:127.0.0.1 -k -s -o /dev/null -w '%{http_code}')
t "healthz" "$(curl "${R[@]}" $U/healthz)" 200
t "unknown path 404 at nginx" "$(curl "${R[@]}" $U/etc/passwd)" 404
t "GET /v1/nodes" "$(curl "${R[@]}" $U/v1/nodes)" 200
t "POST /v1/nodes refused" "$(curl "${R[@]}" -X POST $U/v1/nodes)" 403
t "GET /v1/names/x" "$(curl "${R[@]}" $U/v1/names/abcde)" 200
t "panel login page" "$(curl "${R[@]}" $U/panel/login)" 200
t "tunnel websocket upgrade" "$(curl --http1.1 "${R[@]}" --max-time 3 -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' $U/v1/tunnel)" 101
t "panel login over HTTPS (Secure __Host- cookie)" "$(curl "${R[@]}" -c $d/jar -H "Origin: $U" --data-urlencode username=admin --data-urlencode "password=$PW" $U/panel/login)" 303
grep -q "__Host-cs_panel" $d/jar && t "cookie name" ok ok || t "cookie name" missing ok
for i in 1 2 3 4 5; do curl "${R[@]}" -H "X-Forwarded-For: 9.9.9.$i" -H "Origin: $U" --data-urlencode username=admin --data-urlencode password=wrong-Password-1 $U/panel/login >/dev/null; done
t "spoofed X-Forwarded-For does not reset the login limit" "$(curl "${R[@]}" -H 'X-Forwarded-For: 8.8.8.8' -H "Origin: $U" --data-urlencode username=admin --data-urlencode password=wrong-Password-1 $U/panel/login)" 429
echo "== $pass passed, $fail failed"; exit $fail
