# Iran mirror (broker card 005)

Static files next to the broker; the broker host never executes anything from here.

1. Build and sign a release first (`scripts/release.sh`, see RELEASING.md): the release folder holds the binaries, `SHA256SUMS`, `SHA256SUMS.sig` and `install.sh` (the public key is already inside it).
2. On a machine with international access: `CSNODE_RELEASE=./dist ROOT=/data/mirror ./sync.sh` (rerun any time; resumes). It refuses an unsigned release and puts `install.sh` at `ROOT/../install.sh` and `ROOT/csnode/install.sh`.
3. `rsync -a --partial /data/ SERVER:/srv/` (so `/srv/install.sh` and `/srv/mirror/` both arrive).
4. Add `nginx.conf` to the broker's nginx site. Check both links answer 200: `curl -I https://SERVER/install.sh` and `curl -I https://SERVER/mirror/csnode/install.sh`, and `curl -I https://SERVER/mirror/READY`.

Layout: `apt/debian` (Debian + backports incl. incus), `images/` (simplestreams), `models/<name>/SHA256SUMS`,
`installers/` (WSL rootfs, Windows setup), `csnode/` (binaries, `SHA256SUMS`, `SHA256SUMS.sig`, `install.sh`).
Nodes use it automatically (`csnode setup --mirror auto`), spaces get apt/pip/npm pointed at it (owner may change).
Verify a model: `csnode doctor --model /path/to/models/hermes3-8b-q4_K_M`.
Missing pieces fail with `mirror is missing <path>`.

The mirror is untrusted (client card 014): nodes check Debian packages with the host's own Debian keyring
(no `trusted=yes`), and launch images only by a fingerprint from `images/images.lock`, which must carry
`images/images.lock.sig` made with the release key (`cs release sign images.lock`, on the owner's laptop).
`sync.sh` writes `images.lock.unsigned` for that step. Without a valid signed lock, `csnode setup` stops
when it uses the mirror for images. When upstream publishes a new image, rerun sync and sign a new lock.
