package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"rabbit_example/common"
	"sync"
	"time"
)

// WarehouseService handles inventory updates
type WarehouseService struct {
	inventory     map[string]common.InventoryItem
	inventoryLock sync.RWMutex
	rabbitClient  *common.RabbitMQClient
}

// NewWarehouseService creates a new warehouse service
func NewWarehouseService(rabbitClient *common.RabbitMQClient) *WarehouseService {
	// Initialize with some dummy inventory
	inventory := map[string]common.InventoryItem{
		"product-1": {ProductID: "product-1", Name: "Laptop", Stock: 100, Price: 999.99},
		"product-2": {ProductID: "product-2", Name: "Smartphone", Stock: 200, Price: 599.99},
		"product-3": {ProductID: "product-3", Name: "Headphones", Stock: 150, Price: 99.99},
	}

	return &WarehouseService{
		inventory:    inventory,
		rabbitClient: rabbitClient,
	}
}

// handleOrderPlaced processes an order placed message
func (s *WarehouseService) handleOrderPlaced(messageBody []byte) error {
	var order common.OrderPlaced
	if err := json.Unmarshal(messageBody, &order); err != nil {
		return fmt.Errorf("failed to unmarshal order: %w", err)
	}

	log.Printf("Processing order %s with %d items", order.OrderID, len(order.Items))

	// Update inventory
	s.inventoryLock.Lock()
	defer s.inventoryLock.Unlock()

	// Check if we have enough inventory
	for _, item := range order.Items {
		inventoryItem, exists := s.inventory[item.ProductID]
		if !exists {
			// Publish order status - product not found
			s.publishOrderStatus(order.OrderID, order.CustomerID, "failed",
				fmt.Sprintf("Product %s not found in inventory", item.ProductID))
			return fmt.Errorf("product %s not found in inventory", item.ProductID)
		}

		if inventoryItem.Stock < item.Quantity {
			// Publish order status - insufficient stock
			s.publishOrderStatus(order.OrderID, order.CustomerID, "failed",
				fmt.Sprintf("Insufficient stock for product %s: requested %d, available %d",
					item.ProductID, item.Quantity, inventoryItem.Stock))
			return fmt.Errorf("insufficient stock for product %s: requested %d, available %d",
				item.ProductID, item.Quantity, inventoryItem.Stock)
		}
	}

	// If we have enough inventory, update it
	for _, item := range order.Items {
		inventoryItem := s.inventory[item.ProductID]
		inventoryItem.Stock -= item.Quantity
		s.inventory[item.ProductID] = inventoryItem
		log.Printf("Updated inventory for product %s: new stock %d",
			item.ProductID, inventoryItem.Stock)
	}

	// Publish order status - success
	s.publishOrderStatus(order.OrderID, order.CustomerID, "processed",
		"Order has been processed successfully")

	log.Printf("Order %s processed successfully", order.OrderID)
	return nil
}

// publishOrderStatus publishes the order status to RabbitMQ
func (s *WarehouseService) publishOrderStatus(orderID, customerID, status, message string) {
	orderStatus := common.OrderStatus{
		OrderID:    orderID,
		CustomerID: customerID,
		Status:     status,
		Message:    message,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.rabbitClient.PublishMessage(ctx, common.OrderStatusRoutingKey, orderStatus)
	if err != nil {
		log.Printf("Failed to publish order status: %v", err)
	}
}

// GetInventory returns the current inventory
func (s *WarehouseService) GetInventory(w http.ResponseWriter, r *http.Request) {
	s.inventoryLock.RLock()
	defer s.inventoryLock.RUnlock()

	// Convert map to slice
	inventory := make([]common.InventoryItem, 0, len(s.inventory))
	for _, item := range s.inventory {
		inventory = append(inventory, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inventory)
}

// Health check endpoint
func (s *WarehouseService) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// StartConsumers starts all message consumers
func (s *WarehouseService) StartConsumers() error {
	// Ensure we have a valid exchange name
	exchangeName := s.rabbitClient.Config.Exchange
	if exchangeName == "" {
		exchangeName = "order_exchange" // Fallback to ensure it's not empty
		log.Printf("WARNING: Empty exchange name detected, using fallback: %s", exchangeName)
	}

	log.Printf("Using exchange: '%s'", exchangeName)

	// Start consuming order placed messages
	err := s.rabbitClient.ConsumeMessages(
		common.OrderPlacedQueue,
		common.OrderPlacedRoutingKey,
		s.handleOrderPlaced,
		exchangeName,
	)
	if err != nil {
		return fmt.Errorf("failed to start order placed consumers: %w", err)
	}

	log.Println("Warehouse service is now consuming messages")
	return nil
}
