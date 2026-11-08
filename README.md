# Clothing Store API

Учебный REST API магазина одежды на Go и SQLite. Реализованы каталог товаров, фотографии, пользователи, роли и cookie-аутентификация. Сервер слушает только `127.0.0.1`; не публикуйте его в сети без отдельной настройки HTTPS и проверки безопасности.

## Требования и запуск

- Go версии из `go.mod` (1.27.2 или новее).
- Python нужен только для независимой проверки SQL-схемы.

Скопируйте `.env.example` в `.env`, задайте настройки и из корня проекта выполните:

```bash
go run ./cmd/app
```

Миграции применяются автоматически. Проверка готовности: `GET http://127.0.0.1:8080/health`.

## Настройки

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `APP_PORT` | `8080` | Порт локального HTTP-сервера |
| `DB_PATH` | `./data/clothing-store.db` | Файл SQLite |
| `UPLOAD_DIR` | `./uploads` | Каталог фотографий |
| `LOG_LEVEL` | `info` | Уровень JSON-логирования |
| `AUTH_COOKIE_SECURE` | `false` | Установить `true`, если TLS завершается на доверенном reverse proxy; при прямом HTTPS Secure включается автоматически |

## Первоначальная настройка администратора

Начальная миграция создаёт системные роли, но не создаёт учётную запись администратора. Из интерактивного терминала выполните:

```bash
go run ./cmd/app bootstrap-admin
```

Команда запросит имя, email, телефон, пароль и подтверждение. Пароль вводится без отображения, не передаётся в аргументах командной строки и не записывается в лог. Команда откажет, если администратор уже существует. Не запускайте её из неинтерактивного скрипта.

## Аутентификация

- `POST /auth/register` — регистрация клиента. JSON: `name`, `email`, `phone`, `password`; роль из запроса не принимается. Регистрация устанавливает сессию.
- `POST /auth/login` — вход по `email` и `password`.
- `GET /auth/me` — безопасный профиль текущего пользователя.
- `POST /auth/logout` — завершение текущей сессии.

Сессия длится 12 часов. Клиент получает HttpOnly cookie `clothing_session` и отдельную cookie `clothing_csrf`. Для изменяющих запросов передавайте значение `clothing_csrf` также в заголовке `X-CSRF-Token`. Cookies используют `SameSite=Strict`; `Secure` включается для прямого HTTPS или через `AUTH_COOKIE_SECURE=true` за доверенным TLS proxy. Auth-ответы не кэшируются.

Пароли хранятся только как Argon2id-хеш с индивидуальной случайной солью. Сессии используют случайные 256-битные токены; SQLite хранит только SHA-256 токена и CSRF-значения. При каждой авторизации роль пользователя загружается из БД. Не добавляйте cookies, пароли или токены в логи и не передавайте их в URL.

Вход ограничен десятью попытками с одного IP за 15 минут. Неверный email и неверный пароль возвращают одинаковый `401`. Ограничитель хранится в памяти одного процесса и сбрасывается после перезапуска.

Пример регистрации: `curl` сохраняет сессионную cookie в файл, не печатая заголовки ответа. Не выводите содержимое cookie-файла.

```bash
REGISTER_COOKIE_JAR="$(mktemp)"
curl -sS -c "$REGISTER_COOKIE_JAR" -H 'Content-Type: application/json' \
  -d '{"name":"Test Client","email":"client@example.com","phone":"+70000000000","password":"long example password"}' \
  http://127.0.0.1:8080/auth/register

curl -sS -b "$REGISTER_COOKIE_JAR" http://127.0.0.1:8080/auth/me
rm -f "$REGISTER_COOKIE_JAR"
```

Для изменяющего действия передайте значение CSRF-cookie в заголовке `X-CSRF-Token`; не печатайте cookie jar или его значения в терминал.

## Пользователи и роли

Все операции управления пользователями и изменения ролей доступны только администратору; POST/PUT/DELETE требуют CSRF-заголовок.

- `POST /user` — создать пользователя (поля `name`, `email`, `phone`, `role_id`, `password`).
- `GET /user?limit=20&offset=0` — список пользователей.
- `GET /user/role/{id}?limit=20&offset=0` — список пользователей роли.
- `PUT /user/{id}` — полная замена `name`, `email`, `phone`, `role_id`; пароль меняется отдельными auth-сценариями, хеш не принимается.
- `DELETE /user/{id}` — удалить пользователя; при наличии заказов ответ `409`.
- `GET /role` — список ролей.
- `POST /role`, `PUT /role/{id}`, `DELETE /role/{id}` — управление пользовательскими ролями, только администратор.

Пагинация пользователей: `limit=20`, `offset=0`, максимальный `limit=100`. Email нормализуется в нижний регистр и уникален без учёта регистра. Ответы никогда не включают `password_hash`. Системные роли `client`, `manager`, `admin` нельзя переименовать или удалить. Дополнительные роли не получают прав: авторизация запрещает всё, что явно не разрешено.

## Каталог и фотографии

Гости могут читать `GET /product`, `GET /product/{id}` и `GET /photo/{id}`. Создание, изменение и удаление товаров и фотографий требует роль admin и CSRF-проверку. В каталоге доступны точные фильтры `type`, `gender`, `size`, `color`, `brand`, `season`, пагинация `limit`/`offset` и сортировка `price_asc`/`price_desc`. Цена — целые копейки.

`POST /photo` принимает multipart с полями `product_id` и `file`; поддерживаются JPEG и PNG до 5 MiB и 25 мегапикселей. Файлы лежат в `UPLOAD_DIR`, метаданные — в SQLite. При удалении товара защищается история заказов, а файлы фотографий согласованно перемещаются и восстанавливаются при сбое.

## Корзина и заказы

Изменяющие запросы используют cookie-сессию и CSRF-заголовок `X-CSRF-Token`, равный cookie `clothing_csrf`. Роль и владелец проверяются сервером; ID пользователя в теле запроса не используется для определения владельца.

- `POST /cart` — добавить товар: `{"product_id":"<uuid>","count":2}`. Повторное добавление того же товара увеличивает количество в одной строке корзины.
- `GET /cart/{user_id}` — корзина владельца, включая текущую цену товара; клиент может читать только свою корзину.
- `PUT /cart/{id}` — заменить количество: `{"count":3}`; количество должно быть положительным.
- `DELETE /cart/{id}` — удалить строку корзины.
- `POST /order` — клиент оформляет свою непустую корзину. Ответ содержит заказ и позиции с зафиксированной на момент оформления ценой в копейках. Создание заказа, позиций и очистка корзины проходят одной транзакцией; повтор после успешной очистки получает `409` и не создаёт второй заказ.
- `GET /order?limit=20&offset=0&status=Принят&date_from=2026-01-01&date_to=2026-01-31` — список. Пагинация: `limit` от 1 до 100, `offset` от 0. Статусы ограничены значениями `Принят`, `В работе`, `Собран`, `Готов к выдаче`, `Отменён`. Даты задаются как `YYYY-MM-DD`, включительно в UTC. Клиент получает только собственные заказы; менеджер и администратор — все.
- `GET /order/{id}` — заказ с позициями, суммой и зафиксированными ценами; клиенту доступны только его заказы.
- `PUT /order/{id}` — изменить статус: `{"status":"В работе"}`. Клиент может только отменить собственный заказ со статусом `Принят`. Менеджер и администратор могут проводить допустимые переходы: `Принят → В работе → Собран → Готов к выдаче`; отмена разрешена из `Принят` и `В работе`. Завершённые и отменённые заказы — конечные состояния.
- `POST /order_item` — менеджер или администратор может добавить позицию в заказ со статусом `Принят`: `{"order_id":"<uuid>","product_id":"<uuid>","count":1}`. Цена устанавливается сервером по текущей цене товара.
- `GET /order_item/{order_id}` — позиции заказа доступны владельцу, менеджеру и администратору.

Некорректные поля и фильтры возвращают `400`, отсутствие ресурса — `404`, попытка изменить состояние или оформить пустую корзину — `409`, недостаточные права — `403`. Внутренние детали ошибок базы данных в ответ не включаются. Складской учёт и оплата не реализованы.

## Проверки

```bash
go test -count=1 ./...
go vet ./...
python tests/db_schema_test.py
```

## Пока не реализовано

- Оплата и складской учёт.
- Смена пароля, восстановление доступа, подтверждение email и многофакторная аутентификация.
- Распределённое ограничение входа: текущий limiter действует внутри одного процесса.

Сессии сохраняются в SQLite и отзываются при logout. Не запускайте несколько экземпляров приложения на одной папке `UPLOAD_DIR`; синхронизация файлового хранилища рассчитана на один процесс. Для публичного использования потребуется HTTPS, поскольку REST API должен защищать пароль и сессионные cookie при передаче.

## Примеры curl-запросов

Примеры ниже рассчитаны на Bash и локальный сервер по адресу `http://127.0.0.1:8080`. Перед использованием замените значения в угловых скобках на ID из ответов API и укажите тестовые учётные данные. Для первого администратора сначала выполните `go run ./cmd/app bootstrap-admin` в интерактивном терминале. Роль `manager` создаётся администратором через `POST /user` с системным `role_id`, указанным ниже. Не используйте реальные пароли в примерах или командной строке.

Для запросов с сессионной cookie используйте один cookie jar в той же сессии Bash. Cookie jar содержит секреты сессии: не добавляйте его в Git и удалите после работы. Cookie `clothing_session` передаётся curl автоматически; CSRF-cookie `clothing_csrf` дополнительно передаётся заголовком `X-CSRF-Token` для каждого POST/PUT/DELETE, кроме публичных регистрации и входа.

```bash
BASE_URL='http://127.0.0.1:8080'
COOKIE_JAR="$(mktemp)"
# Удалите cookie jar после демонстрации: rm -f "$COOKIE_JAR"
```

### Аутентификация

`POST /auth/register` и `POST /auth/login` публичны. Регистрация всегда создаёт `client`, поле роли не принимается. Успешные регистрация и вход устанавливают cookies. `GET /auth/me` и `POST /auth/logout` требуют сессию; выход также требует CSRF.

```bash
# Регистрация тестового клиента; пароль замените локальным тестовым значением.
curl -sS -c "$COOKIE_JAR" -H 'Content-Type: application/json' \
  -d '{"name":"Test Client","email":"client@example.test","phone":"+70000000001","password":"<ТЕСТОВЫЙ_ПАРОЛЬ>"}' \
  "$BASE_URL/auth/register"

# Вход администратора, созданного bootstrap-admin; jar обновится администраторской сессией.
curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -H 'Content-Type: application/json' \
  -d '{"email":"<EMAIL_АДМИНИСТРАТОРА>","password":"<ПАРОЛЬ_АДМИНИСТРАТОРА>"}' \
  "$BASE_URL/auth/login"

curl -sS -b "$COOKIE_JAR" "$BASE_URL/auth/me"

CSRF_TOKEN="$(awk '$6 == "clothing_csrf" {print $7}' "$COOKIE_JAR")"
curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
  -H "X-CSRF-Token: $CSRF_TOKEN" "$BASE_URL/auth/logout"
```

В успешном JSON-ответе нет пароля, хеша пароля или токена. Не добавляйте к auth-запросам `-i` при демонстрации: заголовки `Set-Cookie` содержат секреты сессии.

### Публичный каталог и управление товарами

Чтение списка и товара публично. Для создания, полной замены полей через PUT и удаления требуется сессия администратора и CSRF. Цена передаётся целым числом копеек; для PUT укажите все поля товара.

```bash
# Публичные фильтры, пагинация и сортировка по возрастанию цены.
curl -sS --get "$BASE_URL/product" \
  --data-urlencode 'type=outer' --data-urlencode 'gender=unisex' \
  --data-urlencode 'brand=Acme' --data-urlencode 'limit=20' \
  --data-urlencode 'offset=0' --data-urlencode 'sort=price_asc'

curl -sS "$BASE_URL/product/<UUID_ТОВАРА>"

CSRF_TOKEN="$(awk '$6 == "clothing_csrf" {print $7}' "$COOKIE_JAR")"
curl -sS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Jacket","type":"outer","gender":"unisex","size":"M","color":"black","description":"Cotton jacket","brand":"Acme","season":"winter","price":9900}' \
  "$BASE_URL/product"

# PUT выполняет полную замену полей, кроме ID.
curl -sS -b "$COOKIE_JAR" -X PUT -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Winter Jacket","type":"outer","gender":"unisex","size":"M","color":"black","description":"Cotton jacket","brand":"Acme","season":"winter","price":10900}' \
  "$BASE_URL/product/<UUID_ТОВАРА>"

curl -sS -b "$COOKIE_JAR" -X DELETE -H "X-CSRF-Token: $CSRF_TOKEN" \
  "$BASE_URL/product/<UUID_ТОВАРА>"
```

Для списка допустимы точные фильтры `type`, `gender`, `size`, `color`, `brand`, `season`; сортировка — `sort=price_asc` или `sort=price_desc`. `limit` задаётся от 1 до 100, по умолчанию 20; `offset` по умолчанию 0. Неизвестные или неверные параметры возвращают `400`.

### Фотографии

Получение фотографии публично. Загрузка и удаление требуют администратора и CSRF. Файл должен быть доступным локальным JPEG или PNG; действуют лимиты размера и разрешения, описанные выше. Перед запуском подготовьте файл `test-image.png` в текущем каталоге (не добавляя его в репозиторий) либо замените `./test-image.png` в команде на путь к существующему локальному PNG или JPEG; для JPEG укажите `type=image/jpeg`.

```bash
CSRF_TOKEN="$(awk '$6 == "clothing_csrf" {print $7}' "$COOKIE_JAR")"
curl -sS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF_TOKEN" \
  -F 'product_id=<UUID_ТОВАРА>' -F 'file=@./test-image.png;type=image/png' \
  "$BASE_URL/photo"

# GET возвращает бинарные данные изображения, а не JSON.
curl -sS "$BASE_URL/photo/<UUID_ФОТОГРАФИИ>" -o ./photo-download.png

curl -sS -b "$COOKIE_JAR" -X DELETE -H "X-CSRF-Token: $CSRF_TOKEN" \
  "$BASE_URL/photo/<UUID_ФОТОГРАФИИ>"
```

### Пользователи и роли

`GET /role` публичен. Все остальные маршруты этого раздела требуют администратора; POST/PUT/DELETE также требуют CSRF. Для изменения и удаления используйте ID из фактических ответов. Пользователя с историей заказов удалить нельзя (`409`); системные роли нельзя переименовать или удалить.

```bash
curl -sS "$BASE_URL/role"

CSRF_TOKEN="$(awk '$6 == "clothing_csrf" {print $7}' "$COOKIE_JAR")"
curl -sS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"support_test"}' "$BASE_URL/role"

# Создание менеджера администратором; системный ID роли manager задан миграцией.
curl -sS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test Manager","email":"manager@example.test","phone":"+70000000002","role_id":"22222222-2222-4222-8222-222222222222","password":"<ТЕСТОВЫЙ_ПАРОЛЬ>"}' \
  "$BASE_URL/user"

curl -sS -b "$COOKIE_JAR" "$BASE_URL/user?limit=20&offset=0"
curl -sS -b "$COOKIE_JAR" \
  "$BASE_URL/user/role/22222222-2222-4222-8222-222222222222?limit=20&offset=0"

# PUT пользователя заменяет name, email, phone и role_id; пароль в этот запрос не входит.
curl -sS -b "$COOKIE_JAR" -X PUT -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test Manager Updated","email":"manager@example.test","phone":"+70000000002","role_id":"22222222-2222-4222-8222-222222222222"}' \
  "$BASE_URL/user/<UUID_ПОЛЬЗОВАТЕЛЯ>"

curl -sS -b "$COOKIE_JAR" -X DELETE -H "X-CSRF-Token: $CSRF_TOKEN" \
  "$BASE_URL/user/<UUID_ПОЛЬЗОВАТЕЛЯ>"

curl -sS -b "$COOKIE_JAR" -X PUT -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"reviewer"}' \
  "$BASE_URL/role/<UUID_ПОЛЬЗОВАТЕЛЬСКОЙ_РОЛИ>"
curl -sS -b "$COOKIE_JAR" -X DELETE -H "X-CSRF-Token: $CSRF_TOKEN" \
  "$BASE_URL/role/<UUID_ПОЛЬЗОВАТЕЛЬСКОЙ_РОЛИ>"
```

Системные ID ролей: `client` — `11111111-1111-4111-8111-111111111111`, `manager` — `22222222-2222-4222-8222-222222222222`, `admin` — `33333333-3333-4333-8333-333333333333`. Для создания клиента через `POST /user` используйте ID `client`; публичная регистрация сама назначает эту роль.

### Корзина

Все маршруты корзины требуют сессию клиента; клиент может работать только со своей корзиной. POST/PUT/DELETE требуют CSRF. Возьмите `USER_ID` и `CART_ITEM_ID` из ответов `/auth/me` и API корзины.

```bash
CSRF_TOKEN="$(awk '$6 == "clothing_csrf" {print $7}' "$COOKIE_JAR")"
curl -sS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' -d '{"product_id":"<UUID_ТОВАРА>","count":2}' \
  "$BASE_URL/cart"

curl -sS -b "$COOKIE_JAR" "$BASE_URL/cart/<UUID_ПОЛЬЗОВАТЕЛЯ>"
curl -sS -b "$COOKIE_JAR" -X PUT -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' -d '{"count":3}' \
  "$BASE_URL/cart/<UUID_СТРОКИ_КОРЗИНЫ>"
curl -sS -b "$COOKIE_JAR" -X DELETE -H "X-CSRF-Token: $CSRF_TOKEN" \
  "$BASE_URL/cart/<UUID_СТРОКИ_КОРЗИНЫ>"
```

### Заказы и позиции заказа

Оформление, список и чтение заказа доступны клиенту, менеджеру и администратору с учётом области доступа: клиент видит только собственные заказы, менеджер и администратор — все. Клиент создаёт заказ только для своей корзины; POST/PUT требуют CSRF. Клиент может отменить только заказ `Принят`. Менеджер и администратор меняют статусы по разрешённым переходам. Создание позиции заказа доступно менеджеру или администратору, чтение — владельцу заказа, менеджеру или администратору.

```bash
# Клиент оформляет свою непустую корзину; тело не требуется.
CSRF_TOKEN="$(awk '$6 == "clothing_csrf" {print $7}' "$COOKIE_JAR")"
curl -sS -b "$COOKIE_JAR" -X POST -H "X-CSRF-Token: $CSRF_TOKEN" \
  "$BASE_URL/order"

# Список заказов с опциональными фильтрами и пагинацией.
curl -sS -b "$COOKIE_JAR" --get "$BASE_URL/order" \
  --data-urlencode 'limit=20' --data-urlencode 'offset=0' \
  --data-urlencode 'status=Принят' --data-urlencode 'date_from=2026-01-01' \
  --data-urlencode 'date_to=2026-01-31'
curl -sS -b "$COOKIE_JAR" "$BASE_URL/order/<UUID_ЗАКАЗА>"
curl -sS -b "$COOKIE_JAR" "$BASE_URL/order_item/<UUID_ЗАКАЗА>"

# Только менеджер или администратор: добавить позицию в заказ со статусом «Принят».
curl -sS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"order_id":"<UUID_ЗАКАЗА>","product_id":"<UUID_ТОВАРА>","count":1}' \
  "$BASE_URL/order_item"

# Пример менеджерского перехода «Принят» → «В работе».
curl -sS -b "$COOKIE_JAR" -X PUT -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' -d '{"status":"В работе"}' \
  "$BASE_URL/order/<UUID_ЗАКАЗА>"

# Клиент вместо этого может отменить только собственный заказ в статусе «Принят».
curl -sS -b "$COOKIE_JAR" -X PUT -H "X-CSRF-Token: $CSRF_TOKEN" \
  -H 'Content-Type: application/json' -d '{"status":"Отменён"}' \
  "$BASE_URL/order/<UUID_СВОЕГО_ЗАКАЗА>"
```

Допустимые переходы сотрудника: `Принят → В работе` или `Отменён`; `В работе → Собран` или `Отменён`; `Собран → Готов к выдаче`. Статусы `Готов к выдаче` и `Отменён` конечные. Для дат указывайте `YYYY-MM-DD`; границы фильтра включительны в UTC. Пустая корзина при оформлении даёт `409`. Команды здесь иллюстрируют контракт API; для выполнения последовательности войдите в нужную учётную запись (администратор или менеджер для staff-команд, клиент для корзины и оформления) и заново получите CSRF-cookie после входа.
