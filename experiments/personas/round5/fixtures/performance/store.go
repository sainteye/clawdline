package main

import (
	"fmt"
	"sync"
	"time"
)

// queryLatency simulates the round trip to the database for every store call.
const queryLatency = 100 * time.Microsecond

type User struct {
	ID   int
	Name string
}

type Product struct {
	ID   int
	Name string
}

type Order struct {
	ID        int
	UserID    int
	Amount    int // cents
	Tags      []string
	Note      string
	ProductID int
}

// Store stands in for the database. Every method costs one round trip.
type Store struct {
	mu       sync.RWMutex
	users    map[int]User
	products map[int]Product
	orders   []Order
}

func NewStore(nUsers, nOrders int) *Store {
	s := &Store{users: map[int]User{}, products: map[int]Product{}}
	for i := 1; i <= nUsers; i++ {
		s.users[i] = User{ID: i, Name: fmt.Sprintf("User %03d", i)}
	}
	for i := 1; i <= 40; i++ {
		s.products[i] = Product{ID: i, Name: fmt.Sprintf("Product %02d", i)}
	}
	tags := []string{"gift", "express", "bulk", "promo", "fragile", "intl", "retail", "b2b"}
	for i := 1; i <= nOrders; i++ {
		s.orders = append(s.orders, Order{
			ID:        i,
			UserID:    1 + (i*7)%nUsers,
			Amount:    100 + (i*37)%9900,
			Tags:      []string{tags[i%len(tags)], tags[(i*3)%len(tags)], tags[(i*5)%len(tags)]},
			Note:      fmt.Sprintf("  order #%d <for> customer\t%d  ", i, i%13),
			ProductID: 1 + (i*11)%len(s.products),
		})
	}
	return s
}

func (s *Store) roundTrip() { time.Sleep(queryLatency) }

// ListOrders returns every order.
func (s *Store) ListOrders() []Order {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Order, len(s.orders))
	copy(out, s.orders)
	return out
}

// GetUser returns one user.
func (s *Store) GetUser(id int) (User, bool) {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}

// GetUsers returns the users with the given ids in one round trip.
func (s *Store) GetUsers(ids []int) map[int]User {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int]User, len(ids))
	for _, id := range ids {
		if u, ok := s.users[id]; ok {
			out[id] = u
		}
	}
	return out
}

// GetProduct returns one product.
func (s *Store) GetProduct(id int) (Product, bool) {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.products[id]
	return p, ok
}

// GetProducts returns the products with the given ids in one round trip.
func (s *Store) GetProducts(ids []int) map[int]Product {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int]Product, len(ids))
	for _, id := range ids {
		if p, ok := s.products[id]; ok {
			out[id] = p
		}
	}
	return out
}

// CustomerTotal computes one customer's historical spend. CustomerTotals does the same work in
// one store round trip for a set of customers.
func (s *Store) CustomerTotal(userID int) int {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, o := range s.orders {
		if o.UserID == userID {
			total += o.Amount
		}
	}
	return total
}

func (s *Store) CustomerTotals(ids []int) map[int]int {
	s.roundTrip()
	s.mu.RLock()
	defer s.mu.RUnlock()
	wanted := make(map[int]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	out := make(map[int]int, len(ids))
	for _, o := range s.orders {
		if wanted[o.UserID] {
			out[o.UserID] += o.Amount
		}
	}
	return out
}
