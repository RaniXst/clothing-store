package photo

// Photo хранит UUID фотографии, созданное сервером имя файла и UUID товара.
type Photo struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	ProductID string `json:"product_id"`
}
