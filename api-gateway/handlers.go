package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"rabbit_example/common"
	"sync"
	"time"
)

type OrdersCache struct {
	orders     map[string]common.OrderStatus
	ordersLock sync.RWMutex
}

func NewOrdersCache() *OrdersCache {
	return &OrdersCache{
		orders: make(map[string]common.OrderStatus),
	}
}

func (c *OrdersCache) Set(status common.OrderStatus) {
	c.ordersLock.Lock()
	defer c.ordersLock.Unlock()
	c.orders[status.OrderID] = status
}

func (c *OrdersCache) Get(orderID string) (common.OrderStatus, bool) {
	c.ordersLock.RLock()
	defer c.ordersLock.RUnlock()
	status, exists := c.orders[orderID]
	return status, exists
}

type GatewayHandler struct {
	purchaseURL  string
	warehouseURL string
	rabbitClient *common.RabbitMQClient
	ordersCache  *OrdersCache
}

func NewGatewayHandler(purchaseURL, warehouseURL string, rabbitClient *common.RabbitMQClient) *GatewayHandler {
	handler := &GatewayHandler{
		purchaseURL:  purchaseURL,
		warehouseURL: warehouseURL,
		rabbitClient: rabbitClient,
		ordersCache:  NewOrdersCache(),
	}

	// Start order status consumer
	go handler.startOrderStatusConsumer()

	return handler
}

func (h *GatewayHandler) startOrderStatusConsumer() {
	err := h.rabbitClient.ConsumeMessages(
		common.OrderStatusQueue,
		common.OrderStatusRoutingKey,
		h.handleOrderStatus,
	)
	if err != nil {
		log.Fatalf("Failed to start order status consumer: %v", err)
	}
}

func (h *GatewayHandler) handleOrderStatus(messageBody []byte) error {
	var status common.OrderStatus
	if err := json.Unmarshal(messageBody, &status); err != nil {
		return fmt.Errorf("failed to unmarshal order status: %w", err)
	}

	log.Printf("Received order status update for order %s: %s", status.OrderID, status.Status)
	h.ordersCache.Set(status)
	return nil
}

func (h *GatewayHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading req body", http.StatusInternalServerError)
		return
	}
	r.Body.Close()

	purchaseURL := fmt.Sprintf("%s/orders", h.purchaseURL)
	req, err := http.NewRequest("POST", purchaseURL, bytes.NewBuffer(body))
	if err != nil {
		http.Error(w, "Error creating req", http.StatusInternalServerError)
		return
	}

	req.Header = r.Header.Clone()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Purchase service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading response body", http.StatusInternalServerError)
		return
	}
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func (h *GatewayHandler) GetOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderID := r.URL.Query().Get("order_id")
	if orderID == "" {
		http.Error(w, "Missing order_id parameter", http.StatusBadRequest)
		return
	}

	// Try to get order status from cache
	status, exists := h.ordersCache.Get(orderID)
	if !exists {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *GatewayHandler) GetInventory(w http.ResponseWriter, r *http.Request) {
	// Create a reverse proxy
	warehouseURL, err := url.Parse(h.warehouseURL)
	if err != nil {
		http.Error(w, "Error parsing warehouse URL", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(warehouseURL)

	// Update the request URL
	r.URL.Host = warehouseURL.Host
	r.URL.Scheme = warehouseURL.Scheme
	r.URL.Path = "/inventory"
	r.Host = warehouseURL.Host

	// Forward the request to the warehouse service
	proxy.ServeHTTP(w, r)
}

func (h *GatewayHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check purchase service
	purchaseHealth := checkServiceHealth(ctx, h.purchaseURL+"/health")

	// Check warehouse service
	warehouseHealth := checkServiceHealth(ctx, h.warehouseURL+"/health")

	// Return aggregated health status
	health := struct {
		Status           string `json:"status"`
		PurchaseService  string `json:"purchase_service"`
		WarehouseService string `json:"warehouse_service"`
	}{
		PurchaseService:  purchaseHealth,
		WarehouseService: warehouseHealth,
	}

	// Determine overall status
	if purchaseHealth == "healthy" && warehouseHealth == "healthy" {
		health.Status = "healthy"
		w.WriteHeader(http.StatusOK)
	} else {
		health.Status = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func checkServiceHealth(ctx context.Context, url string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "unknown"
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "unhealthy"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return "healthy"
	}
	return "unhealthy"
}
