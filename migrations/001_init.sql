-- 001_init.sql
-- БД магазина одежды. SQLite.
-- UUID генерируются приложением Go и сохраняются как TEXT.
-- Денежные суммы хранятся в копейках (INTEGER).
-- Для каждого соединения SQLite приложение должно выполнять:
-- PRAGMA foreign_keys = ON;

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS roles (
    id   TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL UNIQUE CHECK (length(trim(name)) > 0)
);

CREATE TABLE IF NOT EXISTS products (
    id          TEXT PRIMARY KEY NOT NULL,
    name        TEXT NOT NULL CHECK (length(trim(name)) > 0),
    type        TEXT NOT NULL CHECK (length(trim(type)) > 0),
    gender      TEXT NOT NULL CHECK (length(trim(gender)) > 0),
    size        TEXT NOT NULL CHECK (length(trim(size)) > 0),
    color       TEXT NOT NULL CHECK (length(trim(color)) > 0),
    description TEXT NOT NULL,
    brand       TEXT NOT NULL CHECK (length(trim(brand)) > 0),
    season      TEXT NOT NULL CHECK (length(trim(season)) > 0),
    price       INTEGER NOT NULL CHECK (price >= 0)
);

CREATE TABLE IF NOT EXISTS photos (
    id         TEXT PRIMARY KEY NOT NULL,
    filename   TEXT NOT NULL CHECK (length(trim(filename)) > 0),
    product_id TEXT NOT NULL,
    FOREIGN KEY (product_id) REFERENCES products(id)
        ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS users (
    id                TEXT PRIMARY KEY NOT NULL,
    name              TEXT NOT NULL CHECK (length(trim(name)) > 0),
    email             TEXT NOT NULL COLLATE NOCASE UNIQUE
                      CHECK (length(trim(email)) > 0),
    phone             TEXT NOT NULL CHECK (length(trim(phone)) > 0),
    role_id           TEXT NOT NULL,
    registration_date TEXT NOT NULL,
    password_hash     TEXT NOT NULL CHECK (length(trim(password_hash)) > 0),
    FOREIGN KEY (role_id) REFERENCES roles(id)
        ON UPDATE CASCADE ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS cart (
    id         TEXT PRIMARY KEY NOT NULL,
    user_id    TEXT NOT NULL,
    product_id TEXT NOT NULL,
    count      INTEGER NOT NULL CHECK (count > 0),
    UNIQUE (user_id, product_id),
    FOREIGN KEY (user_id) REFERENCES users(id)
        ON UPDATE CASCADE ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(id)
        ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS orders (
    id       TEXT PRIMARY KEY NOT NULL,
    user_id  TEXT NOT NULL,
    datetime TEXT NOT NULL,
    status   TEXT NOT NULL CHECK (
        status IN ('Принят', 'В работе', 'Собран', 'Готов к выдаче', 'Отменён')
    ),
    FOREIGN KEY (user_id) REFERENCES users(id)
        ON UPDATE CASCADE ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS order_items (
    id         TEXT PRIMARY KEY NOT NULL,
    order_id   TEXT NOT NULL,
    product_id TEXT NOT NULL,
    price      INTEGER NOT NULL CHECK (price >= 0),
    count      INTEGER NOT NULL CHECK (count > 0),
    FOREIGN KEY (order_id) REFERENCES orders(id)
        ON UPDATE CASCADE ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(id)
        ON UPDATE CASCADE ON DELETE RESTRICT
);

-- Индексы для фильтрации и сортировки каталога.
CREATE INDEX IF NOT EXISTS idx_products_type   ON products(type);
CREATE INDEX IF NOT EXISTS idx_products_gender ON products(gender);
CREATE INDEX IF NOT EXISTS idx_products_size   ON products(size);
CREATE INDEX IF NOT EXISTS idx_products_color  ON products(color);
CREATE INDEX IF NOT EXISTS idx_products_brand  ON products(brand);
CREATE INDEX IF NOT EXISTS idx_products_season ON products(season);
CREATE INDEX IF NOT EXISTS idx_products_price  ON products(price);

-- Индексы для связей и основных запросов API.
CREATE INDEX IF NOT EXISTS idx_photos_product_id     ON photos(product_id);
CREATE INDEX IF NOT EXISTS idx_users_role_id         ON users(role_id);
CREATE INDEX IF NOT EXISTS idx_cart_user_id          ON cart(user_id);
CREATE INDEX IF NOT EXISTS idx_cart_product_id       ON cart(product_id);
CREATE INDEX IF NOT EXISTS idx_orders_user_id        ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_datetime       ON orders(datetime);
CREATE INDEX IF NOT EXISTS idx_orders_status         ON orders(status);
CREATE INDEX IF NOT EXISTS idx_order_items_order_id  ON order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_product_id ON order_items(product_id);
