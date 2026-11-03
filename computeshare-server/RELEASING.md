# Releasing ComputeShare

Client and server are folders of one repo (<https://github.com/alishafiee1/ComputeShare>) and are released together with one tag `vX.Y.Z`, one signed `SHA256SUMS`, and one script on the owner's computer:

```bash
scripts/release.sh 0.2.0
```

The full guide is [`../computeshare-client/RELEASING.md`](../computeshare-client/RELEASING.md). The server binary is `csbroker-linux-amd64|arm64` in the same release. To install it on the live server follow "Upgrade" in [`docs/DEPLOY.md`](docs/DEPLOY.md) (checksum and signature check, backup of the state dir, restart), only with the owner's go-ahead (umbrella card 019).
