package order

import (
	"context"
	"github.com/google/uuid"
)

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Checkout(ctx context.Context, actor Actor) (Order, error) {
	if err := requireRole(actor, "client"); err != nil {
		return Order{}, err
	}
	return s.repository.Checkout(ctx, actor.UserID)
}
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
func requireRole(actor Actor, role string) error {
	if err := validActor(actor); err != nil {
		return err
	}
	if actor.Role != role {
		return ErrDenied
	}
	return nil
}
func requireStaff(actor Actor) error {
	if err := validActor(actor); err != nil {
		return err
	}
	if actor.Role != "manager" && actor.Role != "admin" {
		return ErrDenied
	}
	return nil
}
