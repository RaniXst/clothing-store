#!/usr/bin/env python3
"""Smoke/integrity tests for clothing-store SQLite migrations."""
import sqlite3
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "migrations"

def must_fail(conn, sql, params=()):
    try:
        conn.execute(sql, params)
    except sqlite3.IntegrityError:
        return
    raise AssertionError(f"Expected IntegrityError: {sql}")

def run():
    with tempfile.TemporaryDirectory() as td:
        db_path = Path(td) / "test.db"
        conn = sqlite3.connect(db_path)
        conn.execute("PRAGMA foreign_keys = ON")
        conn.executescript((MIGRATIONS / "001_init.sql").read_text(encoding="utf-8"))
        conn.executescript((MIGRATIONS / "002_seed_roles.sql").read_text(encoding="utf-8"))
        conn.executescript((MIGRATIONS / "003_auth_sessions.sql").read_text(encoding="utf-8"))
        assert conn.execute("PRAGMA foreign_keys").fetchone()[0] == 1

        tables = {row[0] for row in conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
        )}
        expected = {"products", "photos", "roles", "users", "cart", "orders", "order_items", "auth_sessions"}
        assert tables == expected, (tables, expected)

        roles = {row[0] for row in conn.execute("SELECT name FROM roles")}
        assert roles == {"client", "manager", "admin"}, roles

        conn.execute("""INSERT INTO products
            (id,name,type,gender,size,color,description,brand,season,price)
            VALUES ('p1','T-shirt','top','unisex','M','black','desc','Brand','summer',199900)""")
        conn.execute("""INSERT INTO users
            (id,name,email,phone,role_id,registration_date,password_hash)
            VALUES ('u1','Test User','test@example.com','+100000000','11111111-1111-4111-8111-111111111111','2026-10-16T10:00:00Z','hash')""")
        conn.execute("INSERT INTO auth_sessions (token_hash,csrf_hash,user_id,created_at,expires_at) VALUES (?,?,'u1','2026-10-16T10:00:00Z','2026-10-16T22:00:00Z')", ('a' * 64, 'b' * 64))
        must_fail(conn, "INSERT INTO auth_sessions (token_hash,csrf_hash,user_id,created_at,expires_at) VALUES ('short','short','missing','now','later')")
        conn.execute("INSERT INTO photos (id,filename,product_id) VALUES ('ph1','p1.jpg','p1')")
        conn.execute("INSERT INTO cart (id,user_id,product_id,count) VALUES ('c1','u1','p1',2)")
        must_fail(conn, "INSERT INTO cart (id,user_id,product_id,count) VALUES ('c2','u1','p1',1)")
        must_fail(conn, "INSERT INTO cart (id,user_id,product_id,count) VALUES ('c3','u1','p1',0)")
        must_fail(conn, """INSERT INTO products
            (id,name,type,gender,size,color,description,brand,season,price)
            VALUES ('p2','Jacket','outer','unisex','L','blue','desc','Brand','winter',-1)""")
        conn.execute("INSERT INTO orders (id,user_id,datetime,status) VALUES ('o1','u1','2026-10-16T10:00:00Z','Принят')")
        conn.execute("INSERT INTO order_items (id,order_id,product_id,price,count) VALUES ('oi1','o1','p1',199900,2)")
        must_fail(conn, "INSERT INTO orders (id,user_id,datetime,status) VALUES ('o2','u1','2026-10-16T10:00:00Z','Unknown')")
        must_fail(conn, """INSERT INTO users
            (id,name,email,phone,role_id,registration_date,password_hash)
            VALUES ('u2','Test User 2','TEST@example.com','+100000001','11111111-1111-4111-8111-111111111111','2026-10-16T10:00:00Z','hash')""")
        must_fail(conn, "DELETE FROM roles WHERE id='11111111-1111-4111-8111-111111111111'")
        must_fail(conn, "DELETE FROM products WHERE id='p1'")

        # Deleting order cascades to order items.
        conn.execute("DELETE FROM orders WHERE id='o1'")
        assert conn.execute("SELECT COUNT(*) FROM order_items WHERE id='oi1'").fetchone()[0] == 0

        # Deleting user cascades to cart.
        conn.execute("DELETE FROM users WHERE id='u1'")
        assert conn.execute("SELECT COUNT(*) FROM cart WHERE id='c1'").fetchone()[0] == 0
        assert conn.execute("SELECT COUNT(*) FROM auth_sessions WHERE user_id='u1'").fetchone()[0] == 0

        # Deleting product cascades to photo metadata and cart entries; now no order references it.
        conn.execute("DELETE FROM products WHERE id='p1'")
        assert conn.execute("SELECT COUNT(*) FROM photos WHERE id='ph1'").fetchone()[0] == 0
        assert conn.execute("PRAGMA foreign_key_check").fetchall() == []

        conn.close()
    print("PASS: migrations, 8 application tables, seed roles, constraints, FK delete rules, foreign_key_check")

if __name__ == "__main__":
    run()
