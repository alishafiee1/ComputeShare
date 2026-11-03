# انتشار نسخهٔ جدید ComputeShare (راهنمای ساده)

کلاینت و سرور دو پوشه از یک مخزن‌اند (<https://github.com/alishafiee1/ComputeShare>) و با **یک برچسب** مثل `v0.2.0`، یک `SHA256SUMS` امضاشده و یک اسکریپت روی کامپیوتر صاحب پروژه منتشر می‌شوند:

```bash
scripts/release.sh 0.2.0
```

راهنمای کامل: [`../computeshare-client/RELEASING.fa.md`](../computeshare-client/RELEASING.fa.md). فایل سرور (`csbroker-linux-amd64` یا `arm64`) در همان انتشار است. نصبش روی سرور واقعی با بخش «Upgrade» در [`docs/DEPLOY.md`](docs/DEPLOY.md) و فقط با اجازهٔ صاحب پروژه انجام می‌شود (کارت 019 چتری).
