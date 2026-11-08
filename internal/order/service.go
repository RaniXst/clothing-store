package order

import (
	"context"
	"github.com/google/uuid"
)

// Service проверяет владельца и роль пользователя перед операциями с заказами.
type Service struct{ repository Repository }

// NewService создаёт службу заказов с переданным хранилищем.
func NewService(repository Repository) *Service { return &Service{repository: repository} }

// Checkout оформляет корзину клиента и создаёт заказ через одну операцию хранилища.
func (s *Service) Checkout(ctx context.Context, actor Actor) (Order, error) {
	if err := requireRole(actor, "client"); err != nil {
		return Order{}, err
	}
	return s.repository.Checkout(ctx, actor.UserID)
}
// List показывает клиенту только его заказы, а менеджеру и администратору — все заказы.
func (s *Service) List(ctx context.Context, actor Actor, options ListOptions) (Page, error) {
	if err := validActor(actor); err != nil {
		return Page{}, err
	}
	switch actor.Role {
	case "client":
		return s.repository.List(ctx, options, actor.UserID)
	case "manager", "admin":
		return s.repository.List(ctx, options, "")
	default:
		return Page{}, ErrDenied
	}
}
// Get проверяет UUID и доступ: клиенту возвращается только его заказ.
func (s *Service) Get(ctx context.Context, actor Actor, id string) (Order, error) {
	if err := validActor(actor); err != nil {
		return Order{}, err
	}
	canonical, err := normalizeID(id)
	if err != nil {
		return Order{}, err
	}
	item, err := s.repository.Get(ctx, canonical)
	if err != nil {
		return Order{}, err
	}
	if actor.Role == "client" && item.UserID != actor.UserID {
		return Order{}, ErrNotFound
	}
	if actor.Role != "client" && actor.Role != "manager" && actor.Role != "admin" {
		return Order{}, ErrDenied
	}
	return item, nil
}

// UpdateStatus разрешает клиенту отменить только свой заказ со статусом «Принят»;
// менеджер и администратор могут менять статус только по разрешённой последовательности.
func (s *Service) UpdateStatus(ctx context.Context, actor Actor, id, newStatus string) (Order, error) {
	if err := validActor(actor); err != nil {
		return Order{}, err
	}
	canonical, err := normalizeID(id)
	if err != nil {
		return Order{}, err
	}
	item, err := s.repository.Get(ctx, canonical)
	if err != nil {
		return Order{}, err
	}
	if actor.Role == "client" {
		if item.UserID != actor.UserID {
			return Order{}, ErrNotFound
		}
		if newStatus != StatusCancelled || item.Status != StatusAccepted {
			return Order{}, ErrConflict
		}
		return s.repository.UpdateStatus(ctx, canonical, item.Status, newStatus)
	}
	if actor.Role != "manager" && actor.Role != "admin" {
		return Order{}, ErrDenied
	}
	if !validTransition(item.Status, newStatus) {
		return Order{}, ErrConflict
	}
	return s.repository.UpdateStatus(ctx, canonical, item.Status, newStatus)
}
// AddItem разрешает менеджеру или администратору добавить проверенную позицию в заказ.
func (s *Service) AddItem(ctx context.Context, actor Actor, input AddItemInput) (Item, error) {
	if err := requireStaff(actor); err != nil {
		return Item{}, err
	}
	orderID, productID, err := input.Validate()
	if err != nil {
		return Item{}, err
	}
	input.OrderID = orderID
	input.ProductID = productID
	return s.repository.AddItem(ctx, input, productID)
}
// Items возвращает позиции заказа только его владельцу, менеджеру или администратору.
func (s *Service) Items(ctx context.Context, actor Actor, orderID string) ([]Item, error) {
	if err := validActor(actor); err != nil {
		return nil, err
	}
	canonical, err := normalizeID(orderID)
	if err != nil {
		return nil, err
	}
	item, err := s.repository.Get(ctx, canonical)
	if err != nil {
		return nil, err
	}
	if actor.Role == "client" && item.UserID != actor.UserID {
		return nil, ErrNotFound
	}
	if actor.Role != "client" && actor.Role != "manager" && actor.Role != "admin" {
		return nil, ErrDenied
	}
	return s.repository.Items(ctx, canonical)
}

// validTransition разрешает «Принят» → «В работе» или «Отменён»,
// «В работе» → «Собран» или «Отменён», «Собран» → «Готов к выдаче».
// Статусы «Готов к выдаче» и «Отменён» являются конечными.
func validTransition(from, to string) bool {
	switch from {
	case StatusAccepted:
		return to == StatusWorking || to == StatusCancelled
	case StatusWorking:
		return to == StatusAssembled || to == StatusCancelled
	case StatusAssembled:
		return to == StatusReady
	}
	return false
}
// validActor проверяет UUID пользователя и наличие одной из трёх известных ролей.
func validActor(actor Actor) error {
	if _, err := uuid.Parse(actor.UserID); err != nil {
		return ErrDenied
	}
	switch actor.Role {
	case "client", "manager", "admin":
		return nil
	}
	return ErrDenied
}
// requireRole проверяет корректность пользователя и требует указанную роль.
func requireRole(actor Actor, role string) error {
	if err := validActor(actor); err != nil {
		return err
	}
	if actor.Role != role {
		return ErrDenied
	}
	return nil
}
// requireStaff допускает к операции только менеджера или администратора.
func requireStaff(actor Actor) error {
	if err := validActor(actor); err != nil {
		return err
	}
	if actor.Role != "manager" && actor.Role != "admin" {
		return ErrDenied
	}
	return nil
}
