package product_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/photo"
	"github.com/RaniXst/clothing-store/internal/product"
	"github.com/RaniXst/clothing-store/internal/storage"
	"github.com/RaniXst/clothing-store/migrations"
)

type productTestApp struct {
	router *chi.Mux
	db     *sql.DB
	files  *storage.Local
}

func newProductTestApp(t *testing.T) productTestApp {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	productRepo := product.NewSQLRepository(db)
	photoRepo := photo.NewSQLRepository(db)
	service := product.NewService(productRepo, photoRepo, files, &sync.Mutex{})
	router := chi.NewRouter()
	product.NewHandler(service).RegisterRoutes(router)
	t.Cleanup(func() { _ = db.Close() })
	return productTestApp{router: router, db: db, files: files}
}

func productRequest(app productTestApp, method, target string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	app.router.ServeHTTP(recorder, request)
	return recorder
}

func productBody(name, brand string, price int64) []byte {
	return []byte(fmt.Sprintf(`{"name":%q,"type":"outer","gender":"unisex","size":"M","color":"black","description":"cotton","brand":%q,"season":"winter","price":%d}`, name, brand, price))
}

func TestProductCRUDAndFullReplacement(t *testing.T) {
	app := newProductTestApp(t)
	created := productRequest(app, http.MethodPost, "/product", productBody("Jacket", "Acme", 9900))
	if created.Code != http.StatusCreated {
		t.Fatalf("POST /product = %d, body %s", created.Code, created.Body.String())
	}
	var item product.Product
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(item.ID); err != nil {
		t.Fatalf("server-generated id %q is not a UUID: %v", item.ID, err)
	}
	if created.Header().Get("Location") != "/product/"+item.ID {
		t.Fatalf("Location = %q", created.Header().Get("Location"))
	}

	updated := productRequest(app, http.MethodPut, "/product/"+item.ID, productBody("Coat", "Acme", 12900))
	if updated.Code != http.StatusOK {
		t.Fatalf("PUT /product/{id} = %d, body %s", updated.Code, updated.Body.String())
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Name != "Coat" || item.Price != 12900 {
		t.Fatalf("PUT did not replace product fields: %+v", item)
	}

	got := productRequest(app, http.MethodGet, "/product/"+item.ID, nil)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"name":"Coat"`) {
		t.Fatalf("GET /product/{id} = %d, body %s", got.Code, got.Body.String())
	}
	deleted := productRequest(app, http.MethodDelete, "/product/"+item.ID, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("DELETE /product/{id} = %d, body %s", deleted.Code, deleted.Body.String())
	}
	missing := productRequest(app, http.MethodGet, "/product/"+item.ID, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("GET deleted product = %d, want 404", missing.Code)
	}
}

func TestProductListFiltersPaginationAndSort(t *testing.T) {
	app := newProductTestApp(t)
	for _, item := range []struct {
		name, kind, brand string
		price             int64
	}{
		{"A", "outer", "Acme", 300}, {"B", "outer", "Acme", 100},
		{"C", "outer", "Acme", 200}, {"D", "top", "Other", 50},
	} {
		body := productBody(item.name, item.brand, item.price)
		var object map[string]any
		_ = json.Unmarshal(body, &object)
		object["type"] = item.kind
		body, _ = json.Marshal(object)
		response := productRequest(app, http.MethodPost, "/product", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s: status %d, body %s", item.name, response.Code, response.Body.String())
		}
	}

	response := productRequest(app, http.MethodGet, "/product?brand=Acme&type=outer&limit=1&offset=1&sort=price_asc", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET list = %d, body %s", response.Code, response.Body.String())
	}
	var page product.ProductPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].Price != 200 || page.Limit != 1 || page.Offset != 1 {
		t.Fatalf("unexpected filtered page: %+v", page)
	}

	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?sort=price%3BDROP", "?brand=", "?unknown=x"} {
		bad := productRequest(app, http.MethodGet, "/product"+query, nil)
		if bad.Code != http.StatusBadRequest {
			t.Errorf("GET /product%s = %d, want 400", query, bad.Code)
		}
	}
}

func TestProductValidationAndHistoryDeletionPreservePhoto(t *testing.T) {
	app := newProductTestApp(t)
	invalid := productRequest(app, http.MethodPost, "/product", []byte(`{"name":"","price":1}`))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid create = %d, want 400", invalid.Code)
	}
	created := productRequest(app, http.MethodPost, "/product", productBody("Jacket", "Acme", 9900))
	var item product.Product
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}

	photoID := uuid.NewString()
	filename := photoID + ".png"
	temp, err := app.files.Prepare([]byte("placeholder"))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.files.Publish(temp, filename); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec("INSERT INTO photos (id, filename, product_id) VALUES (?, ?, ?)", photoID, filename, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO users
		(id,name,email,phone,role_id,registration_date,password_hash)
		VALUES ('u1','User','user@example.com','+100000000','11111111-1111-4111-8111-111111111111','2026-10-18T00:00:00Z','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec("INSERT INTO orders (id,user_id,datetime,status) VALUES ('o1','u1','2026-10-18T00:00:00Z','Принят')"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec("INSERT INTO order_items (id,order_id,product_id,price,count) VALUES ('oi1','o1',?,9900,1)", item.ID); err != nil {
		t.Fatal(err)
	}
	blocked := productRequest(app, http.MethodDelete, "/product/"+item.ID, nil)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("delete product with order history = %d, want 409", blocked.Code)
	}
	if _, err := app.files.Read(filename); err != nil {
		t.Fatalf("photo file was not restored after blocked deletion: %v", err)
	}
	if got := productRequest(app, http.MethodGet, "/product/"+item.ID, nil).Code; got != http.StatusOK {
		t.Fatalf("product was deleted despite history: status %d", got)
	}
}
