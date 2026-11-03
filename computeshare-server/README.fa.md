# computeshare-server

[English](README.md)

بخش سرور **ComputeShare**: اشتراک CPU/RAM/GPU سیستم‌های خانگی از راه SSH، بدون IP ثابت و بدون نیاز به اعتماد به سرور.

`csbroker` یک فایل اجرایی Go است که:
- نودهایی را که **خودشان به سرور وصل می‌شوند** (تونل WSS) می‌پذیرد؛ صاحب نود به IP ثابت یا باز کردن پورت مودم نیاز ندارد؛
- SSH کاربران را فقط از **یک پورت (2222)** با ProxyJump به نود می‌رساند؛ رمزنگاری سرتاسری است، سرور رمز یا محتوا را نمی‌بیند و هیچ کدی اجرا نمی‌کند؛
- نودها را با توکن عضویت (و در صورت نیاز تأیید مدیر) ثبت می‌کند و کلید میزبانشان را سنجاق می‌کند تا نود جعلی رد شود؛
- فهرست نودها را با مشخصات و بار فعلی/میانگین نشان می‌دهد؛
- **پنل وب مدیریت** (`/panel/`) با رمز قوی، محدودیت تلاش ورود و لاگ ممیزی دارد؛
- مهاجم‌ها را محدود و مسدود می‌کند و `X-Forwarded-For` را فقط از `trusted_proxies` قبول می‌کند.

نرم‌افزار نود و ابزار کاربر در مخزن [computeshare-client](https://github.com/alishafiee1/computeshare-client) است.

- راهنمای نصب سرور: [docs/DEPLOY.fa.md](docs/DEPLOY.fa.md)
- هدف‌ها و وضعیت: [GOALS.md](GOALS.md) · انتشار نسخه: [RELEASING.fa.md](RELEASING.fa.md)
- لایسنس: **AGPL-3.0** · نام و لوگو: [TRADEMARK.md](TRADEMARK.md) · امنیت: [SECURITY.md](SECURITY.md) · مشارکت: [CONTRIBUTING.md](CONTRIBUTING.md)
