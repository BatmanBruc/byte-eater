# Bot Converter

Telegram бот для конвертации файлов в различные форматы.

## Возможности

- Конвертация файлов (документы/изображения/аудио/видео/электронные книги/шрифты)
- Конвертация текста в файл (через выбор формата в кнопках)
- Очередь задач и хранение состояния в Redis

## Требования

- Docker и Docker Compose
- `BOT_TOKEN` от BotFather
- Go **1.25+** (только если запускаете без Docker)

## Установка и запуск

### Через Docker (рекомендуется)

1. Создайте `config.env` в корне проекта (можно скопировать из `config.env.example`):

```bash
# Linux/macOS
cp config.env.example config.env

# Windows (PowerShell)
Copy-Item config.env.example config.env
```

```env
BOT_TOKEN=YOUR_BOT_TOKEN_FROM_BOTFATHER
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=CHANGE_ME
REDIS_DB=0
```

2. Запустите проект:

```bash
docker-compose up -d --build
```

Важно:

- `REDIS_PASSWORD` **обязателен** для запуска через `docker-compose` (см. команду Redis в `docker-compose.yml`).
- `config.env` добавлен в `.gitignore` — **не коммитьте** токены/пароли.

3. Проверьте логи:

```bash
docker-compose logs -f bot-converter
```

### Локальный запуск (без Docker)

1. Поднимите Redis (можно локально) и задайте переменные окружения:

- `BOT_TOKEN`
- `REDIS_HOST` (по умолчанию `localhost`)
- `REDIS_PORT` (по умолчанию `6379`)
- `REDIS_PASSWORD` (если Redis с паролем)
- `REDIS_DB` (по умолчанию `0`)

2. Запустите бота:

```bash
go run .
```

## Остановка

```bash
docker-compose down
```

Для удаления данных Redis и временных файлов:

```bash
docker-compose down -v
```

## Установленные утилиты в контейнере

- **ffmpeg** - для конвертации видео и аудио
- **ImageMagick** (magick/convert) - для конвертации изображений
- **LibreOffice** (libreoffice/soffice) - для конвертации документов Office
- **Calibre** (ebook-convert) - для конвертации электронных книг
- **poppler-utils** (pdftotext, pdftohtml) - для работы с PDF

## Поддерживаемые форматы

- Изображения: PNG, JPG, JPEG, JP2, WEBP, BMP, TIF, TIFF, GIF, ICO, HEIC, AVIF, TGS, PSD, SVG, APNG, EPS
- Аудио: MP3, OGG, OPUS, WAV, FLAC, WMA, OGA, M4A, AAC, AIFF, AMR
- Видео: MP4, AVI, WMV, MKV, 3GP, 3GPP, MPG, MPEG, WEBM, TS, MOV, FLV, ASF, VOB
- Документы: XLSX, XLS, TXT, RTF, DOC, DOCX, ODT, PDF, ODS, TORRENT
- Презентации: PPT, PPTX, PPTM, PPS, PPSX, PPSM, POT, POTX, POTM, ODP
- Электронные книги: EPUB, MOBI, AZW3, LRF, PDB, CBR, FB2, CBZ, DJVU
- Шрифты: TTF, OTF, EOT, WOFF, WOFF2, SVG, PFB

## Использование

1. Отправьте команду `/start` боту
2. Отправьте файл (документ/фото/видео/аудио) или текст
3. Выберите целевой формат в появившихся кнопках
4. Дождитесь результата

Для текста доступны форматы: `TXT`, `PDF`, `DOCX`, `RTF`, `ODT`.

Используйте `/help` для просмотра всех поддерживаемых форматов.

