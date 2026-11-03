# راهنمای نصب سرور csbroker (نسخهٔ v2، خلاصهٔ فارسی)

نسخهٔ کامل با همهٔ دستورها: [DEPLOY.md](DEPLOY.md). هیچ رمز یا توکنی در این فایل نیست.

## چه چیزی در v2 عوض شد

- **دیگر برای هر نود پورت جدا باز نمی‌شود.** همهٔ SSH فقط از **پورت 2222** است: `ssh -J join@node.n3xus.ir:2222 user@<نود>` (یا `ssh <نود>.cs`). کلیدهای `port_range` و `port_bind`، تنظیم sysctl و قانون ufw برای `40000-40199` حذف شدند.
- API ثبت‌نام، تونل و پنل در **`https://node.n3xus.ir`** پشت nginx روی پورت 443 است و فقط مسیرهای لازم عبور می‌کنند (`deploy/nginx/node.n3xus.ir.conf`).
- `trusted_proxies`: فقط nginx اجازه دارد IP واقعی کاربر را بفرستد؛ کسی نمی‌تواند IP خودش را جعل کند. وقتی nginx آدرس ثابت گرفت، فهرست را به همان یک آدرس محدود کنید، چون هر کانتینر دیگری روی `172.18.0.0/16` هم می‌تواند IP جعل کند.
- محدودیت نرخ (کارت 008): سهمیهٔ جدا برای SSH (۶۰ در دقیقه)، تونل نود (۱۲) و API (۶۰). پر شدن سهمیه فقط همان درخواست را رد می‌کند و ban نمی‌کند؛ ban ده‌دقیقه‌ای فقط برای رفتار بد است (۱۰ handshake ناموفق یا ۱۰ درخواست برای نود ناموجود در یک دقیقه، یا ۵ توکن نامعتبر در ده دقیقه).
- **پنل وب مدیریت** در `/panel/` و امکان «تأیید مدیر» برای نودهای جدید.


## قدم‌ها به زبان ساده

1. **بررسی:** پورت‌های 2222 و 8088 آزاد باشند (`ss -tlnp`).
2. **پشتیبان:** از تنظیمات فایروال و پوشهٔ `/var/lib/csbroker` نسخهٔ پشتیبان بگیرید؛ پوشهٔ state (پایگاه‌دادهٔ SQLite) را **فقط با سرویس متوقف** کپی کنید (`systemctl stop csbroker` ← کپی ← `systemctl start csbroker`).
3. **فایل برنامه:** `sha256sum -c --ignore-missing SHA256SUMS` باید `OK` بدهد؛ بعد فایل را در `/usr/local/bin/csbroker` نصب کنید.
4. **کاربر سرویس:** کاربر سیستمی `csbroker` بسازید.
5. **تنظیمات:** `deploy/broker/csbroker.yaml` را در `/etc/csbroker.yaml` بگذارید. مثلاً: `http: 172.18.0.1:8088` و `trusted_proxies: 127.0.0.1/32, 172.18.0.0/16`.
6. **سرویس:** `deploy/broker/csbroker.service` را نصب و `systemctl enable --now csbroker` کنید. اگر nginx در Docker است، فایل `deploy/systemd/10-docker-bridge.conf` را در `/etc/systemd/system/csbroker.service.d/` بگذارید تا csbroker بعد از Docker (و همراه آن) اجرا شود، چون IP پل Docker قبل از آن وجود ندارد.
7. **رمز پنل و توکن:** `sudo -u csbroker csbroker admin set-password --state /var/lib/csbroker` (رمز حداقل ۱۲ حرف، از سه نوع حرف/عدد/نماد). روی ترمینال رمز دو بار پرسیده می‌شود؛ اگر از فایل یا pipe بدهید فقط **یک خط** خوانده می‌شود و تکرار نمی‌خواهد (مثلاً `< /root/csbroker-panel-admin.pass`). توکن عضویت را مستقیم در فایل 0600 بنویسید یا از پنل بسازید (فقط یک بار نمایش داده می‌شود).
8. **nginx:** رکورد DNS `node.n3xus.ir`، گواهی TLS، و بلوک 443 از `deploy/nginx/node.n3xus.ir.conf`.
   - اگر nginx قبلاً بلوک پورت 80 دارد (مثل رودو)، فقط `node.n3xus.ir` را به `server_name` همان بلوک اضافه کنید (مسیر `/.well-known/acme-challenge/` برای گواهی بماند)؛ بلوک 80 تکراری نسازید.
   - اگر nginx داخل کانتینر است و فقط یک فایل `nginx.conf` را mount کرده (مثلاً: `/path/to/deploy/nginx.conf`)، بلوک را به همان فایل اضافه کنید و بعد **کانتینر را restart کنید** (`docker restart nginx-proxy-1`)؛ reload کافی نیست.
   - **خطر:** هر deploy رودو این فایل را بازنویسی می‌کند و بلوک نود پاک می‌شود. بهتر است بلوک را در یک **فایل جدا** بگذارید و جدا mount کنید (مثلاً در `docker-compose.prod.yml` به `/etc/nginx/conf.d/node.n3xus.ir.conf`)، یا به فایل nginx مخزن رودو اضافه کنید.
   - برای بررسی `trusted_proxies`: در `journalctl -u csbroker` هر درخواست `/v1/` یک خط `http request` با `client_ip` (IP واقعی کاربر) و `remote_addr` (IP کانتینر nginx) دارد.
9. **فایروال:** `ufw limit 2222/tcp`. اگر nginx در Docker است، فایروال ترافیک کانتینر به میزبان را می‌بندد؛ پس فقط از شبکهٔ Docker اجازه دهید: `ufw allow from 172.18.0.0/16 to 172.18.0.1 port 8088 proto tcp`. پورت 8088 را برای هیچ‌کس دیگری باز نکنید. اگر از v1 مانده: `ufw delete allow 40000:40199/tcp`.
10. **مودم:** فقط TCP 2222 را به IP داخلی سرور هدایت کنید.
11. **آزمون:** `curl -s https://node.n3xus.ir/healthz` باید `ok` بدهد و `https://node.n3xus.ir/panel/login` صفحهٔ ورود را نشان دهد.

## ارتقا از v1

توقف سرویس و پشتیبان از `/var/lib/csbroker` → نصب فایل جدید → حذف `port_bind` و `port_range` از تنظیمات و افزودن کلیدهای جدید (اگر nginx در Docker است حتماً `http: 172.18.0.1:8088`، نه پیش‌فرض `127.0.0.1:8088`) → قانون ufw برای 8088 از `172.18.0.0/16` و فایل drop-in سیستم‌دی → حذف `/etc/sysctl.d/60-csbroker.conf` و قانون ufw بازهٔ پورت‌ها → `systemctl restart csbroker` → گذاشتن رمز پنل. پایگاه‌داده خودکار ارتقا پیدا می‌کند. کلید میزبان SSH را نگه دارید وگرنه همهٔ کاربرها هشدار می‌گیرند. نودها هم باید به کلاینت v2 ارتقا پیدا کنند.

## برگشت

سرویس را غیرفعال کنید، فایل‌های سرویس و تنظیمات و برنامه را پاک کنید، `ufw delete limit 2222/tcp`، و بلوک `node.n3xus.ir` را از nginx بردارید.
