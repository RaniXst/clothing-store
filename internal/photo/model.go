package photo

// Photo хранит UUID фотографии, созданное сервером имя файла и UUID товара.
// Метаданные и байты разделены. SQLite хранит связь с товаром, а файл можно отдавать
// браузеру отдельно от ответа каталога.
type Photo struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	ProductID string `json:"product_id"`
}
