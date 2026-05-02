# gdrivedl

[English](README.md) | فارسی

`gdrivedl` یک کتابخانه و ابزار خط فرمان نوشته‌شده با Go برای دانلود فایل‌ها و پوشه‌های اشتراکی Google Drive است.

ویژگی‌ها:

- دانلود فایل‌های اشتراکی بدون OAuth
- دانلود پوشه‌های اشتراکی برای لینک‌های عمومی، با یا بدون API key
- دانلودهای قابل‌ازسرگیری برای فایل‌های اشتراکی بزرگ
- نمایش اختیاری پیشرفت کلی همراه با سرعت و زمان تقریبی باقی‌مانده
- نمایش وضعیت اتصال هر فایل در نمای پیشرفت
- دانلود هم‌زمان برای فهرست URLها و دانلود پوشه‌ها
- گزارش نهایی اختیاری با وضعیت تکمیل هر فایل
- گزارش تکمیل اختیاری برای هر فایل بعد از دانلود موفق
- حالت آزمایشی اختیاری (`dry-run`) برای تست درخواست بدون ذخیره‌سازی فایل‌ها
- اسکن اختیاری اتصال با مقادیر قابل‌استفادهٔ مجدد برای fronting targetها، IPها و پروفایل‌های `uTLS`
- خروجی رویدادهای ساختاریافته همراه با زمان برای یکپارچه‌سازی با GUI و استفاده از CLI در حالت JSON
- زمان‌انتظار HTTP قابل‌تنظیم و لاگ‌های لایهٔ انتقال با سطح جزئیات قابل‌کنترل
- تأخیر قابل‌تنظیم بین درخواست‌های HTTP
- امکان dump کردن هدرهای درخواست و پاسخ HTTP
- domain fronting برای HTTP
- پشتیبانی از پراکسی‌های HTTP و SOCKS5
- پروفایل‌های `uTLS` ClientHello برای درخواست‌های HTTPS
- ترجیح اختیاری HTTP/2، اجبار به HTTP/1.1 و اشتراک اتصال TLS برای HTTP/2
- بازنویسی IP سرور برای هر درخواست با `--resolve-to`
- دانلود چندگانه از فایل فهرست URL یا ورودی استاندارد

## نصب CLI

```bash
go install github.com/hadi77ir/gdrivedl/cmd/gdrivedl@latest
```

## استفاده به‌عنوان پکیج Go

ماژول اصلی را در برنامهٔ Go دیگری import کنید:

```go
import "github.com/hadi77ir/gdrivedl"
```

نمونهٔ ساده:

```go
ctx := context.Background()
err := gdrivedl.Download(ctx, gdrivedl.Request{
    URL:     "https://drive.google.com/file/d/FILE_ID/view?usp=sharing",
    WorkDir: "/tmp/downloads",
    Proxy:   "http://127.0.0.1:2089",
})
```

برای یکپارچه‌سازی با رابط‌های کاربری، از `gdrivedl.DownloadWithObserver` استفاده کنید تا snapshotهای دوره‌ای وظایف و به‌روزرسانی‌های پیشرفت کلی را دریافت کنید.

برای یکپارچه‌سازی پیشرفته‌تر، `gdrivedl.Request.EventObserver` را تنظیم کنید تا رکوردهای زمان‌دار `Event` را برای لاگ‌ها، به‌روزرسانی‌های پیشرفت و گزارش‌ها دریافت کنید.

## API Key

دانلود پوشه‌های عمومی اشتراکی به Google Drive API key نیاز ندارد.
API keyها همچنان برای دانلودهای قابل‌ازسرگیری و جست‌وجوی metadata از طریق Drive API استفاده می‌شوند.

`gdrivedl` کلید را از این منابع می‌خواند:

- `--api-key`
- `GDRIVEDL_APIKEY`
- `GOODLS_APIKEY` به‌عنوان fallback قدیمی

مثال:

```bash
export GDRIVEDL_APIKEY='your-api-key'
```

## استفادهٔ پایه

دانلود یک فایل اشتراکی:

```bash
gdrivedl get -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

دانلود از لینک‌های اشتراکی فقط وقتی کار می‌کند که لینک بدون نیاز به ورود تعاملی Google به‌صورت عمومی در دسترس باشد.

دانلود یک فایل Google Docs به‌صورت متن ساده:

```bash
gdrivedl get -u 'https://docs.google.com/document/d/FILE_ID/edit?usp=sharing' -e txt
```

دانلود یک پوشهٔ عمومی اشتراکی بدون API key:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing'
```

دانلود یک پوشهٔ اشتراکی با API key:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --api-key "$GDRIVEDL_APIKEY"
```

نمایش metadata/فهرست پوشه بدون API key:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --file-info
```

نمایش metadata فایل با API key:

```bash
gdrivedl get -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing' --api-key "$GDRIVEDL_APIKEY" --file-info
```

اجرای یک دانلود قابل‌ازسرگیری:

```bash
gdrivedl get -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing' --api-key "$GDRIVEDL_APIKEY" --resumable-download 100m
```

ازسرگیری دانلود یک پوشه و نگه‌داشتن فایل‌های کامل‌شده:

```bash
gdrivedl get -u 'https://drive.google.com/drive/folders/FOLDER_ID?usp=sharing' --api-key "$GDRIVEDL_APIKEY" --resumable-download 100m
```

- فایل‌های کامل‌شده نگه داشته و رد می‌شوند.
- فایل‌های ناقص، در صورت پشتیبانی از دانلود بازه‌ای، ازسرگرفته می‌شوند.
- اگر فایلی قابل‌ازسرگیری نباشد، `gdrivedl` فایل ناقص را موقتاً حفظ می‌کند و همان فایل را از ابتدا دوباره دانلود می‌کند، بدون این‌که کل فرایند ازسرگیری پوشه شکست بخورد.
- با فشردن `Ctrl+C` کار جاری از طریق context مشترک لغو می‌شود، نزدیک‌ترین وضعیت امنِ جزئی روی دیسک حفظ می‌شود و بعداً می‌توانید با `-r` دوباره اجرا کنید تا از نزدیک‌ترین نقطهٔ ممکن ادامه پیدا کند.

تست یک درخواست دانلود بدون نوشتن هیچ فایلی:

```bash
gdrivedl get --dry-run -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

ارسال رویدادهای JSON ساختاریافته به‌جای لاگ‌های متنی معمولی:

```bash
gdrivedl get --json -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

دانلود چند URL از یک فایل:

```bash
gdrivedl get --url-list urls.txt
```

دانلود چند URL از ورودی استاندارد:

```bash
cat urls.txt | gdrivedl get --url-list -
```

دانلود یک فهرست به‌صورت هم‌زمان همراه با پیشرفت و گزارش نهایی:

```bash
gdrivedl get \
  --url-list urls.txt \
  --concurrency 4 \
  --progress \
  --exit-report \
  --proxy 'http://127.0.0.1:2089'
```

## اسکن و تست

بررسی مسیرهای مستقیم و fronted با اسکنر مستقل:

```bash
gdrivedl scan
```

اجرای فقط فاز کشف IP:

```bash
gdrivedl scan --scan-mode only-ip
```

اجرای فقط فاز دامنه/SNI روی IPهایی که قبلاً پیدا شده‌اند:

```bash
gdrivedl scan \
  --scan-mode only-domains \
  --resolve-to 203.0.113.10,203.0.113.11 \
  --fronting-target google.com \
  --fronting-sni www.google.com
```

اضافه‌کردن دامنه‌های کاندید بیشتر از فایل یا ورودی استاندارد:

```bash
gdrivedl scan --scan-domain-list extra-domains.txt
cat extra-domains.txt | gdrivedl scan --scan-domain-list -
```

اضافه‌کردن IPها یا بازه‌های IPv4 CIDR بیشتر، یا نمونه‌گیری تصادفی از هر CIDR:

```bash
gdrivedl scan --scan-ip-list extra-ips.txt --scan-ip-random-count 16
```

ارسال یک probe انتقال بدون شروع دانلود:

```bash
gdrivedl test --fronting-enable --fronting-target google.com --utls-profile firefox_auto
```

ذخیرهٔ نتایج قابل‌استفادهٔ مجدد اسکن در یک فایل YAML، همراه با ادغام در صورت وجود فایل:

```bash
gdrivedl scan --save gdrivedl.yml
```

اسکنر `https://gstatic.com/generate_204` را probe می‌کند و مقادیر قابل‌استفادهٔ مجدد زیر را گزارش می‌دهد:

- `--fronting-target`
- `--fronting-sni`
- `--resolve-to`
- `--utls-profile`

`--scan-mode full` هر دو فاز را به‌صورت ترتیبی اجرا می‌کند. `--scan-mode only-ip` نتایج DNS، مقادیر پیکربندی‌شدهٔ `--resolve-to` و بازه‌های IP را اسکن می‌کند. `--scan-mode only-domains` از کشف DNS/IP صرف‌نظر می‌کند و targetهای fronting را به‌همراه هر hostname اضافی در `--fronting-sni` روی فهرست IPهای داده‌شده در `--resolve-to` probe می‌کند.

`--save` نتایج اسکن را دوباره در یک فایل پیکربندی YAML می‌نویسد. این گزینه بخش‌های نامرتبط مانند `defaults`، `get` و `merge` را حفظ می‌کند و در عین حال مقادیر قابل‌استفادهٔ مجدد در `transport` و `scan` مانند `fronting-target`، `fronting-sni`، `resolve-to` و `utls-profile` را به‌روزرسانی می‌کند.

در صورت وجود، فهرست subdomainهای کاندید از `assets/google-subdomains.txt` بارگذاری می‌شود؛ در غیر این صورت از فهرست تعبیه‌شده استفاده می‌شود و می‌توان آن را با `--scan-domain-list` گسترش داد. همچنین IPها و بازه‌های CIDR مربوط به Google از `assets/google-ips.txt` بارگذاری می‌شوند و می‌توان آن‌ها را با `--scan-ip-list` گسترش داد.

## ادغام

ادغام chunkها از پوشهٔ فعلی در یک فایل خروجی:

```bash
gdrivedl merge -o merged.bin
```

ادغام چند پوشهٔ `split_nnnn` در یک فایل خروجی:

```bash
gdrivedl merge -o merged.bin split_0001 split_0002 split_0003
```

ادغام از یک پوشهٔ والد که شامل زیرپوشه‌های `split_nnnn` است:

```bash
gdrivedl merge -o merged.bin ./download-parts
```

حذف chunkها فقط بعد از تکمیل موفق ادغام امن:

```bash
gdrivedl merge -o merged.bin --delete-chunks ./download-parts
```

استفاده از حالت قدیمی streaming که مستقیماً روی فایل نهایی می‌نویسد و هم‌زمان chunkها را حذف می‌کند:

```bash
gdrivedl merge -o merged.bin --unsafe ./download-parts
```

نکات:

- `gdrivedl merge` به‌طور پیش‌فرض از حالت امن استفاده می‌کند: ابتدا در یک خروجی موقت می‌نویسد، بعد از موفقیت آن را به جای فایل نهایی rename می‌کند و تا وقتی `--delete-chunks` تنظیم نشده باشد chunkها را نگه می‌دارد.
- `gdrivedl merge --unsafe` رفتار قدیمیِ نوشتن مستقیم را برمی‌گرداند و هنگام ادغام chunkها را حذف می‌کند، اما در این حالت لغو عملیات پشتیبانی نمی‌شود.

## پیکربندی YAML

هر زیرفرمان از `--config` پشتیبانی می‌کند.

- مسیر پیش‌فرض: `$XDG_CONFIG_DIR/gdrivedl.yml`
- مسیر fallback: `$XDG_CONFIG_HOME/gdrivedl.yml` و سپس `gdrivedl.yml` در پوشهٔ تنظیمات کاربر
- غیرفعال‌کردن صریح بارگذاری: `--config ''`

مثال:

```yaml
defaults:
  json: false

transport:
  proxy: http://127.0.0.1:2089
  timeout: 45s
  retry-count: 2
  verbosity: 1

get:
  progress: true
  concurrency: 4
  directory: /tmp/downloads
  api-key: your-api-key
  fronting-enable: true
  fronting-target: google.com
  utls-profile: firefox_auto,chrome_auto

scan:
  scan-mode: full
  fronting-enable: true
  fronting-target: google.com
  scan-ip-random-count: 16

merge:
  progress: true
  delete-chunks: false
```

فایل‌های پیکربندی عمداً ورودی‌های یک‌بارهٔ CLI مانند `url`، `url-list`، `help`، `version` یا آرگومان‌های ورودی `merge` را نمی‌پذیرند.

## پیشرفت و گزارش‌دهی

`--progress`

- فایل فعلی، وضعیت فعلی اتصال/دانلود آن، پیشرفت کلی، سرعت و زمان تقریبی باقی‌مانده را نشان می‌دهد.

`--concurrency`

- حداکثر تعداد دانلودهای هم‌زمان را تعیین می‌کند.
- برای دانلودهای `--url-list` و دانلود پوشه‌ها اعمال می‌شود.
- مقدار پیش‌فرض `1` است.

`--exit-report`

- یک گزارش نهایی برای هر فایل شامل نام فایل، درصد دانلودشده و وضعیت نهایی چاپ می‌کند.

`--completion-report`

- بعد از هر دانلود موفق، یک خط گزارش تکمیل برای همان فایل چاپ می‌کند.

`--json`

- در هر خط یک شیء JSON تولید می‌کند.
- شامل رویدادهای زمان‌دار `log`، `progress` و `report` است.
- مصرف خروجی CLI را برای wrapperهای GUI و اتوماسیون ساده‌تر می‌کند.

`--no-json`

- حتی اگر `json: true` از config آمده باشد، خروجی JSON را غیرفعال می‌کند.

`--dry-run`

- درخواست دانلود را بدون ذخیره‌سازی بدنهٔ پاسخ روی دیسک ارسال می‌کند.
- فایل یا پوشه‌ای ایجاد نمی‌کند.
- برای تست اتصال، مسیر‌دهی درخواست و دسترس‌پذیری مقصد مفید است.

بازنویسی‌های منفی رایج:

- `--no-progress` هم پیشرفت کلی و هم progress bar قدیمیِ تک‌فایلی را غیرفعال می‌کند.
- `--no-exit-report` و `--no-completion-report` خروجی گزارش به‌ارث‌رسیده از config را غیرفعال می‌کنند.
- `--no-fronting`، `--no-prefer-http2` و `--no-force-http1` booleanهای لایهٔ انتقال را که از config به ارث رسیده‌اند غیرفعال می‌کنند.
- `--safe` حالت `unsafe` مربوط به merge را که از config آمده باشد غیرفعال می‌کند.
- `--keep-chunks` گزینهٔ `delete-chunks` مربوط به merge را که از config آمده باشد غیرفعال می‌کند.

نکات:

- `--progress` را نمی‌توان همراه با `--no-progress` استفاده کرد.
- `--concurrency` باید بزرگ‌تر از `0` باشد.

## لاگ‌گیری HTTP

`--timeout`

- زمان‌انتظار HTTP client را تنظیم می‌کند.
- رشته‌های مدت‌زمان Go مانند `30s`، `2m` و `1h` را می‌پذیرد.
- اعداد صحیح ساده به‌عنوان ثانیه در نظر گرفته می‌شوند.

`--request-delay`

- حداقل تأخیر بین درخواست‌های HTTP را تنظیم می‌کند.
- رشته‌های مدت‌زمان Go مانند `500ms`، `2s` و `1m` را می‌پذیرد.
- اعداد صحیح ساده به‌عنوان ثانیه در نظر گرفته می‌شوند.

`--verbosity`

- `0` لاگ مراحل را غیرفعال می‌کند.
- `1` لاگ مراحل اتصال برای هر درخواست و لاگ وضعیت HTTP را فعال می‌کند.
- `2` لاگ‌های جزئی‌تر اتصال مانند targetهای dial resolved شده و پایان دریافت بدنهٔ پاسخ را اضافه می‌کند.

`--dump-request`

- درخواست‌های HTTP خروجی را قبل از ارسال dump می‌کند.

`--dump-response`

- هدرهای پاسخ HTTP دریافتی را بعد از دریافت dump می‌کند.

مراحل اتصال شامل نمونه‌هایی مانند این‌ها است:

- `resolving`
- `dialing`
- `proxy connect`
- `request delay`
- `tls handshake`
- `sending request`
- `waiting for response`
- `response headers received`
- `downloading`

مثال:

```bash
gdrivedl get \
  --progress \
  --verbosity 2 \
  --timeout 45s \
  --request-delay 1s \
  --dump-request \
  --dump-response \
  --proxy 'http://127.0.0.1:2089' \
  -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

## لایهٔ انتقال شبکه

### پراکسی

با `--proxy` از پراکسی upstream استفاده کنید.

schemeهای پشتیبانی‌شده:

- `http://`
- `socks5://`

نمونهٔ پراکسی HTTP:

```bash
gdrivedl get --proxy 'http://127.0.0.1:2089' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `--retry-count`

مشخص می‌کند `gdrivedl` بعد از تلاش اولیه چند بار retry کند.

- خطاهای گذرای شبکه را retry می‌کند.
- پاسخ‌های HTTP `408`، `425`، `429`، `500`، `502`، `503` و `504` را retry می‌کند.
- مقدار پیش‌فرض `0` است.

مثال:

```bash
gdrivedl get --retry-count 2 --proxy 'http://127.0.0.1:2089' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `uTLS`

درخواست‌های HTTPS از `uTLS` استفاده می‌کنند. پروفایل ClientHello را با `--utls-profile` انتخاب کنید.

وقتی domain fronting فعال باشد، `gdrivedl` به‌طور خودکار handshakeهای جایگزین `uTLS` را retry می‌کند. اگر برای ترافیک fronted یک پروفایل صریح می‌خواهید، `firefox_auto` معمولاً انتخاب اولیهٔ خوبی است.

مقادیر پشتیبانی‌شده:

- `chrome_auto`
- `firefox_auto`
- `safari_auto`
- `ios_auto`
- `edge_auto`
- `360_auto`
- `qq_auto`
- `randomized`
- `randomized_alpn`
- `randomized_no_alpn`

مثال:

`--utls-profile` یک پروفایل یا یک فهرست comma-separated را می‌پذیرد که به‌صورت round-robin استفاده می‌شود.

```bash
gdrivedl get --utls-profile 'firefox_auto,chrome_auto' -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### انتخاب نسخهٔ HTTP

`--prefer-http2`

- در صورت موفقیت ALPN negotiation، HTTP/2 را نسبت به HTTP/1.1 ترجیح می‌دهد.
- اگر HTTP/2 در دسترس نباشد به HTTP/1.1 fallback می‌کند.

`--force-http1`

- برای درخواست‌های HTTPS، HTTP/1.1 را اجباری می‌کند.
- HTTP/2 negotiation را غیرفعال می‌کند.
- با `--prefer-http2` یا `--share-http2-connection` قابل‌ترکیب نیست.

`--share-http2-connection`

- یک اتصال TLS مبتنی بر HTTP/2 را برای چند درخواست به همان مقصد reuse می‌کند.
- ترجیح HTTP/2 را implied می‌کند.
- با `--force-http1` قابل‌ترکیب نیست.

مثال:

```bash
gdrivedl get \
  --prefer-http2 \
  --share-http2-connection \
  --utls-profile chrome_auto \
  --proxy 'http://127.0.0.1:2089' \
  -u 'https://drive.google.com/file/d/FILE_ID/view?usp=sharing'
```

### `--resolve-to`

IP مورد استفاده برای network dial را بازنویسی می‌کند، در حالی که port اصلی درخواست و logical host حفظ می‌شوند.

می‌توانید یک فهرست comma-separated از IPها بدهید. درخواست‌ها از آن فهرست به‌صورت round-robin انتخاب می‌کنند.

مثال:

```bash
gdrivedl get --resolve-to 203.0.113.10 -u 'https://www.googleapis.com/drive/v3/files/FILE_ID?alt=media'
```

### Domain Fronting

`--fronting-enable`

- domain fronting برای HTTP را در shared transport برای همهٔ درخواست‌ها فعال می‌کند.

`--fronting-target`

- hostname هدف fronting که برای network dial استفاده می‌شود.
- port اصلی درخواست حفظ می‌شود.
- یک فهرست comma-separated از hostnameها را می‌پذیرد و بین آن‌ها به‌صورت round-robin می‌چرخد.
- به `--fronting-enable` نیاز دارد.

`--fronting-sni`

- بازنویسی اختیاری TLS SNI برای درخواست‌های fronted.
- در صورت عدم تنظیم، به `--fronting-target` پیش‌فرض می‌شود.
- به `--fronting-enable` نیاز دارد.

مثال:

```bash
gdrivedl get \
  --fronting-enable \
  --fronting-target front.example.com \
  --fronting-sni front.example.com \
  --proxy 'http://127.0.0.1:2089' \
  --utls-profile chrome_auto \
  -u 'https://www.googleapis.com/drive/v3/files/FILE_ID?alt=media'
```

## نکات

- دانلود پوشه‌های عمومی اشتراکی بدون API key کار می‌کند.
- لینک‌های مستقیم تو‌در‌توی پوشه‌های عمومی ممکن است metadata مربوط به عنوان پوشه را ارائه نکنند؛ در این حالت، `gdrivedl` برای نام پوشهٔ سطح بالا از شناسهٔ پوشه استفاده می‌کند، در حالی که همچنان محتوای پوشه را دانلود می‌کند.
- فایل‌های پروژهٔ Google Apps Script از لینک‌های پوشه قابل دانلود نیستند.
- هنگام دانلود پوشه‌ها، `-m` بر اساس `mimeType` مبدأ Drive فیلتر می‌کند.

## راهنما

```bash
gdrivedl --help
gdrivedl help get
gdrivedl help scan
```

## یادداشت پایانی

این کد با کمک دستیارهای هوش مصنوعی، از جمله Codex و GPT-5.4، توسعه داده شده است.
