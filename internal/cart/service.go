package cart

import (
	"context"
	"fmt"
)

// Service проверяет права клиента и правила корзины перед работой с хранилищем.
type Service struct{ repository Repository }

// NewService создаёт службу корзины с переданным хранилищем.
func NewService(repository Repository) *Service { return &Service{repository: repository} }

// Add разрешает клиенту добавить товар после проверки UUID и количества.
func (s *Service) Add(ctx context.Context, actor Actor, input AddInput) (Item, error) {
	if err := requireClient(actor); err != nil {
		return Item{}, err
	}
	productID, err := input.Validate()
	if err != nil {
		return Item{}, err
	}
	input.ProductID = productID
	return s.repository.Add(ctx, actor.UserID, input)
}

// List возвращает корзину только тогда, когда запрошенный UUID совпадает с UUID клиента в сессии.
func (s *Service) List(ctx context.Context, actor Actor, userID string) ([]Item, error) {
	if err := requireClient(actor); err != nil {
		return nil, err
	}
	requested, err := normalizeID(userID)
	if err != nil {
		return nil, err
	}
	if requested != actor.UserID {
		return nil, ErrNotFound
	}
	return s.repository.List(ctx, requested)
}
// Update проверяет владельца, UUID позиции и положительное количество перед изменением.
func (s *Service) Update(ctx context.Context, actor Actor, cartID string, count int64) (Item, error) {
	if err := requireClient(actor); err != nil {
		return Item{}, err
	}
	if count < 1 {
		return Item{}, fmt.Errorf("%w: count must be positive", ErrInvalid)
	}
	id, err := normalizeID(cartID)
	if err != nil {
		return Item{}, err
	}
	return s.repository.Update(ctx, actor.UserID, id, count)
}
// Delete разрешает клиенту удалить только свою позицию корзины.
func (s *Service) Delete(ctx context.Context, actor Actor, cartID string) error {
	if err := requireClient(actor); err != nil {
		return err
	}
	id, err := normalizeID(cartID)
	if err != nil {
		return err
	}
	return s.repository.Delete(ctx, actor.UserID, id)
}
// requireClient проверяет, что действие выполняет клиент с корректным UUID.
// Корзина принадлежит покупателю. Роли сотрудников могут управлять заказами, но не получают
// прав менять корзину клиента.
func requireClient(actor Actor) error {
	if actor.Role != "client" {
		return ErrDenied
	}
	if _, err := normalizeID(actor.UserID); err != nil {
		return fmt.Errorf("%w: invalid session user", ErrDenied)
	}
	return nil
}
