package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"rabbit_example/common"
	"time"

	"github.com/google/uuid"
)

// PurchaseService handles order creation
type PurchaseService struct {
	rabbitClient *common.RabbitMQClient
}

// NewPurchaseService creates a new purchase service
func NewPurchaseService(rabbitClient *common.RabbitMQClient) *PurchaseService {
	return &PurchaseService{
		rabbitClient: rabbitClient,
	}
}

// PlaceOrderRequest is the request for placing an order
type PlaceOrderRequest struct {
	CustomerID string        `json:"customer_id"`
	Items      []common.Item `json:"items"`
}

// PlaceOrderResponse is the response for placing an order
type PlaceOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// PlaceOrder handles the order placement
func (s *PurchaseService) PlaceOrder(w http.ResponseWriter, r *http.Request) {
	var request PlaceOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if request.CustomerID == "" || len(request.Items) == 0 {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Create order
	orderID := uuid.New().String()
	totalPrice := calculateTotalPrice(request.Items)
	currentTime := time.Now().Format(time.RFC3339)

	order := common.OrderPlaced{
		OrderID:    orderID,
		CustomerID: request.CustomerID,
		Items:      request.Items,
		TotalPrice: totalPrice,
		CreatedAt:  currentTime,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Make sure the exchange exists before publishing
	log.Printf("Publishing order to exchange %s with routing key %s",
		s.rabbitClient.Config.Exchange, common.OrderPlacedRoutingKey)

	// Publish order to RabbitMQ
	err := s.rabbitClient.PublishMessage(ctx, common.OrderPlacedRoutingKey, order)
	if err != nil {
		log.Printf("Failed to publish order: %v", err)
		http.Error(w, "Failed to process order", http.StatusInternalServerError)
		return
	}

	// Also publish initial order status
	orderStatus := common.OrderStatus{
		OrderID:    orderID,
		Status:     "pending",
		Message:    "Order received and processing",
		CustomerID: request.CustomerID,
		CreatedAt:  currentTime,
	}

	err = s.rabbitClient.PublishMessage(ctx, common.OrderStatusRoutingKey, orderStatus)
	if err != nil {
		log.Printf("Failed to publish order status: %v", err)
	}

	// Return response
	response := PlaceOrderResponse{
		OrderID: orderID,
		Status:  "pending",
		Message: "Order placed successfully and sent to warehouse",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// Health check endpoint
func (s *PurchaseService) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// calculateTotalPrice calculates the total price of an order
func calculateTotalPrice(items []common.Item) float64 {
	var total float64
	for _, item := range items {
		total += item.Price * float64(item.Quantity)
	}
	return total
}
